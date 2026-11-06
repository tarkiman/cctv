package discover

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrAuth    = errors.New("camera rejected the login")
	ErrNoMedia = errors.New("camera reported no media profiles")
)

// probeONVIF multicasts a WS-Discovery Probe for video transmitters and collects
// answers until timeout. Keyed by the answering IP.
func probeONVIF(ctx context.Context, timeout time.Duration) map[string]ONVIF {
	out := map[string]ONVIF{}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return out
	}
	defer conn.Close()
	msgID := make([]byte, 16)
	rand.Read(msgID)
	probe := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+
		`<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" `+
		`xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">`+
		`<e:Header><w:MessageID>uuid:%x-%x-%x-%x-%x</w:MessageID><w:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>`+
		`<w:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action></e:Header>`+
		`<e:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></e:Body></e:Envelope>`,
		msgID[0:4], msgID[4:6], msgID[6:8], msgID[8:10], msgID[10:16])
	dst := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 3702}
	conn.WriteToUDP([]byte(probe), dst)

	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	buf := make([]byte, 64<<10)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return out
		}
		body := string(buf[:n])
		o := ONVIF{XAddr: tag(body, "XAddrs")}
		scopes := tag(body, "Scopes")
		o.Name = scope(scopes, "name")
		o.Hardware = scope(scopes, "hardware")
		if o.XAddr != "" {
			o.XAddr = xaddrRe.FindString(o.XAddr)
			out[addr.IP.String()] = o
		}
	}
}

func tag(xmlText, name string) string {
	re := regexp.MustCompile(`<(?:[A-Za-z0-9]+:)?` + name + `[^>]*>([^<]*)<`)
	if m := re.FindStringSubmatch(xmlText); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func scope(scopes, key string) string {
	for _, s := range strings.Fields(scopes) {
		if v, ok := strings.CutPrefix(s, "onvif://www.onvif.org/"+key+"/"); ok {
			u, err := url.PathUnescape(v)
			if err != nil {
				return v
			}
			return u
		}
	}
	return ""
}

// Stream is one media profile of a camera.
type Stream struct {
	Profile  string `json:"profile"`
	URL      string `json:"url"` // without credentials
	Encoding string `json:"encoding,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

var httpClient = &http.Client{Timeout: 6 * time.Second}

// ONVIFStreams asks an ONVIF camera for its RTSP stream URLs, largest resolution first.
func ONVIFStreams(ctx context.Context, xaddr, username, password string) ([]Stream, error) {
	media := xaddr
	if body, err := soap(ctx, xaddr, "", "", `<GetCapabilities xmlns="http://www.onvif.org/ver10/device/wsdl"><Category>Media</Category></GetCapabilities>`); err == nil {
		if m := regexp.MustCompile(`(?s)<(?:\w+:)?Media>.*?<(?:\w+:)?XAddr>([^<]+)<`).FindStringSubmatch(body); m != nil {
			media = strings.TrimSpace(m[1])
		}
	}
	body, err := soap(ctx, media, username, password, `<GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/>`)
	if err != nil {
		return nil, err
	}
	var env struct {
		Profiles []struct {
			Token string `xml:"token,attr"`
			Name  string `xml:"Name"`
			Video struct {
				Encoding   string `xml:"Encoding"`
				Resolution struct {
					Width  int `xml:"Width"`
					Height int `xml:"Height"`
				} `xml:"Resolution"`
			} `xml:"VideoEncoderConfiguration"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	if err := xml.Unmarshal([]byte(body), &env); err != nil {
		return nil, fmt.Errorf("GetProfiles: %w", err)
	}
	if len(env.Profiles) == 0 {
		return nil, ErrNoMedia
	}
	var out []Stream
	for _, p := range env.Profiles {
		req := `<GetStreamUri xmlns="http://www.onvif.org/ver10/media/wsdl"><StreamSetup>` +
			`<Stream xmlns="http://www.onvif.org/ver10/schema">RTP-Unicast</Stream>` +
			`<Transport xmlns="http://www.onvif.org/ver10/schema"><Protocol>RTSP</Protocol></Transport>` +
			`</StreamSetup><ProfileToken>` + xmlEscape(p.Token) + `</ProfileToken></GetStreamUri>`
		b, err := soap(ctx, media, username, password, req)
		if err != nil {
			continue
		}
		uri := tag(b, "Uri")
		if uri == "" {
			continue
		}
		if u, err := url.Parse(uri); err == nil {
			u.User = nil
			uri = u.String()
		}
		out = append(out, Stream{Profile: p.Name, URL: uri, Encoding: p.Video.Encoding, Width: p.Video.Resolution.Width, Height: p.Video.Resolution.Height})
	}
	if len(out) == 0 {
		return nil, ErrNoMedia
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Width*out[i].Height > out[j].Width*out[j].Height })
	return out, nil
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// soap posts a SOAP 1.2 request, with a WS-Security UsernameToken (PasswordDigest) when a
// username is given.
func soap(ctx context.Context, endpoint, username, password, body string) (string, error) {
	header := ""
	if username != "" {
		nonce := make([]byte, 16)
		rand.Read(nonce)
		created := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		h := sha1.New()
		h.Write(nonce)
		h.Write([]byte(created))
		h.Write([]byte(password))
		header = `<s:Header><Security s:mustUnderstand="1" xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">` +
			`<UsernameToken><Username>` + xmlEscape(username) + `</Username>` +
			`<Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` +
			base64.StdEncoding.EncodeToString(h.Sum(nil)) + `</Password>` +
			`<Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` +
			base64.StdEncoding.EncodeToString(nonce) + `</Nonce>` +
			`<Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">` + created + `</Created>` +
			`</UsernameToken></Security></s:Header>`
	}
	env := `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">` + header + `<s:Body>` + body + `</s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(env))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	text := string(b)
	if resp.StatusCode == http.StatusUnauthorized || strings.Contains(text, "NotAuthorized") || strings.Contains(text, "FailedAuthentication") {
		return "", ErrAuth
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("ONVIF %s: HTTP %d", endpoint, resp.StatusCode)
	}
	return text, nil
}
