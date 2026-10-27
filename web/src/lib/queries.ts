import { QueryClient, useQuery } from '@tanstack/react-query'
import { fetchQuestions, fetchReturned, fetchUsage, fetchVersion } from './api'

export const USAGE_INTERVAL_MS = 30_000
export const FORGE_INTERVAL_MS = 300_000

export const queryKeys = {
  questions: ['questions'],
  returned: ['returned'],
  usage: ['usage'],
  health: ['health'],
} as const

export function useQuestions() {
  return useQuery({
    queryKey: queryKeys.questions,
    queryFn: ({ signal }) => fetchQuestions(signal),
    refetchInterval: FORGE_INTERVAL_MS,
  })
}

export function useReturned() {
  return useQuery({
    queryKey: queryKeys.returned,
    queryFn: ({ signal }) => fetchReturned(signal),
    refetchInterval: FORGE_INTERVAL_MS,
  })
}

export function useUsage() {
  return useQuery({
    queryKey: queryKeys.usage,
    queryFn: ({ signal }) => fetchUsage(signal),
    refetchInterval: USAGE_INTERVAL_MS,
  })
}

export function useVersion() {
  return useQuery({
    queryKey: queryKeys.health,
    queryFn: ({ signal }) => fetchVersion(signal),
  })
}

// retry is off: the interval is the retry, and a retry delays the error message.
export function newQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}
