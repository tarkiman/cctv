<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { MapPinOff } from 'lucide-vue-next'
import StateMessage from '../components/StateMessage.vue'
</script>

<template>
  <StateMessage :icon="MapPinOff" title="Halaman tidak ditemukan" text="Alamat yang Anda buka tidak ada.">
    <RouterLink to="/" class="rounded-lg bg-primary px-4 py-2 font-semibold text-on-primary">Ke halaman Live</RouterLink>
  </StateMessage>
</template>
