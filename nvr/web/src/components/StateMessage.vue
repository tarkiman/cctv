<script setup lang="ts">
// Shared empty / error state block.
import type { Component } from 'vue'

defineProps<{ icon: Component; title: string; text?: string; tone?: 'muted' | 'danger' }>()
</script>

<template>
  <div class="mx-auto flex max-w-md flex-col items-center py-12 text-center" :role="tone === 'danger' ? 'alert' : undefined">
    <component :is="icon" class="mb-3 size-10" :class="tone === 'danger' ? 'text-danger' : 'text-muted'" aria-hidden="true" />
    <h2 class="text-base font-semibold">{{ title }}</h2>
    <p v-if="text" class="mt-1 text-sm text-muted">{{ text }}</p>
    <div class="mt-4"><slot /></div>
  </div>
</template>
