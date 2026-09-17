<script setup lang="ts">
// `/app/tier1/:path*` (CHRN-58): one page of the generated estate wiki,
// full-width, via the reusable Tier1Pane. `getTier1Page`'s `path` is
// relative and slash-separated (openapi.yaml's Tier1PagePath) -- exactly
// the joined route param below.
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Tier1Pane from '@/components/Tier1Pane.vue'
import { api } from '@/api/client'
import type { components } from '@/api/schema.d.ts'

type Tier1Page = components['schemas']['Tier1Page']

const route = useRoute()

const page = ref<Tier1Page | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)

const path = computed(() => {
  const raw = route.params.pathMatch
  return Array.isArray(raw) ? raw.filter(Boolean).join('/') : (raw ?? '')
})

// Bumped on every load and captured per-request, so a slower answer to an
// earlier path cannot land after a faster one and show the wrong page under
// the sidebar's currently-highlighted tier-1 row (two rows clicked quickly).
let loadSeq = 0

async function load(): Promise<void> {
  const seq = ++loadSeq
  page.value = null
  error.value = null
  if (!path.value) {
    loading.value = false
    error.value = 'No tier-1 page named. Pick one from the sidebar.'
    return
  }
  loading.value = true
  const res = await api.GET('/tier1/page', { params: { query: { path: path.value } } })
  if (seq !== loadSeq) return // superseded by a later request
  loading.value = false
  if (res.data) {
    page.value = res.data
    return
  }
  if (res.response.status === 404) {
    error.value = `No generated page at ${path.value}.`
  } else {
    error.value = res.error?.message ?? 'The generated page could not be read.'
  }
}

watch(path, load, { immediate: true })
</script>

<template>
  <div class="ch-tier1-route">
    <Tier1Pane :page="page" :loading="loading" :error="error" />
  </div>
</template>

<style scoped>
.ch-tier1-route {
  padding: 34px 46px;
  max-width: 900px;
}
</style>
