import { QueryClient, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import {
  ApiError,
  fetchCleanup,
  fetchPriorities,
  fetchQuestions,
  fetchReturned,
  fetchRuns,
  fetchUsage,
  fetchVersion,
  type PrioritiesData,
  saveMilestoneRanks,
} from './api'
import type { RankChange } from './priorities/moves'
import { setRank } from './priorities/rank'

export const USAGE_INTERVAL_MS = 30_000
export const FORGE_INTERVAL_MS = 300_000
export const QUESTIONS_INTERVAL_MS = 15_000

export const queryKeys = {
  questions: ['questions'],
  returned: ['returned'],
  cleanup: ['cleanup'],
  runs: ['runs'],
  priorities: ['priorities'],
  usage: ['usage'],
  health: ['health'],
} as const

export function useQuestions() {
  return useQuery({
    queryKey: queryKeys.questions,
    queryFn: ({ signal }) => fetchQuestions(signal),
    refetchInterval: QUESTIONS_INTERVAL_MS,
    staleTime: QUESTIONS_INTERVAL_MS,
  })
}

export function useReturned() {
  return useQuery({
    queryKey: queryKeys.returned,
    queryFn: ({ signal }) => fetchReturned(signal),
    refetchInterval: QUESTIONS_INTERVAL_MS,
    staleTime: QUESTIONS_INTERVAL_MS,
  })
}

export function useCleanup() {
  return useQuery({
    queryKey: queryKeys.cleanup,
    queryFn: ({ signal }) => fetchCleanup(signal),
    refetchInterval: FORGE_INTERVAL_MS,
    staleTime: FORGE_INTERVAL_MS,
  })
}

export function usePriorities() {
  return useQuery({
    queryKey: queryKeys.priorities,
    queryFn: ({ signal }) => fetchPriorities(signal),
    refetchInterval: FORGE_INTERVAL_MS,
    staleTime: FORGE_INTERVAL_MS,
  })
}

export const RANK_CONFLICT_TEXT = 'De volgorde is net ergens anders veranderd; de lijst is ververst.'

const milestoneRanksKey = ['milestone-ranks'] as const

export interface RankVars {
  repo: string
  changes: RankChange[]
}

// Returns the priorities with the rank changes of one repo written into the descriptions.
function withRanks(data: PrioritiesData, { repo, changes }: RankVars): PrioritiesData {
  const to = new Map(changes.map((c) => [c.number, c.to]))
  return {
    ...data,
    repos: data.repos.map((r) =>
      r.slug !== repo
        ? r
        : {
            ...r,
            milestones: r.milestones.map((m) => {
              const rank = to.get(m.number)
              return rank === undefined ? m : { ...m, description: setRank(m.description, rank) }
            }),
          },
    ),
  }
}

function rankErrorText(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409) return RANK_CONFLICT_TEXT
    return `Opslaan mislukt: ${err.detail === '' ? `HTTP ${err.status}` : err.detail}`
  }
  return 'Opslaan mislukt: server onbereikbaar'
}

// Writes milestone ranks optimistically. The pending repos are tracked here, not through
// useMutation().isPending/variables, which only describe the latest call; two repos may be in
// flight at once. The list is refetched once the last write has settled, so a refetch for one
// repo never wipes the optimistic ranks of another that is still in flight.
export function useMilestoneRanks() {
  const client = useQueryClient()
  const [pending, setPending] = useState<ReadonlySet<string>>(() => new Set())
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationKey: milestoneRanksKey,
    mutationFn: ({ repo, changes }: RankVars) => saveMilestoneRanks(repo, changes),
    onMutate: async (vars) => {
      setPending((s) => new Set(s).add(vars.repo))
      setError(null)
      await client.cancelQueries({ queryKey: queryKeys.priorities })
      client.setQueryData<PrioritiesData>(queryKeys.priorities, (old) => (old === undefined ? old : withRanks(old, vars)))
    },
    onError: (err) => {
      setError(rankErrorText(err))
    },
    onSettled: async (_data, _err, vars) => {
      setPending((s) => {
        const next = new Set(s)
        next.delete(vars.repo)
        return next
      })
      // This mutation still counts as running here.
      if (client.isMutating({ mutationKey: milestoneRanksKey }) === 1) {
        await client.invalidateQueries({ queryKey: queryKeys.priorities })
      }
    },
  })

  const { mutate } = mutation
  const isPending = useCallback((repo: string) => pending.has(repo), [pending])
  return { mutate, isPending, error }
}

// The runs come from the tick state in the local database: no forge cost, so the usage interval.
export function useRuns() {
  return useQuery({
    queryKey: queryKeys.runs,
    queryFn: ({ signal }) => fetchRuns(signal),
    refetchInterval: USAGE_INTERVAL_MS,
    staleTime: USAGE_INTERVAL_MS,
  })
}

export function useUsage() {
  return useQuery({
    queryKey: queryKeys.usage,
    queryFn: ({ signal }) => fetchUsage(signal),
    refetchInterval: USAGE_INTERVAL_MS,
    staleTime: USAGE_INTERVAL_MS,
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
