import { QueryClient, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useRef, useState } from 'react'
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

export const RANK_CONFLICT_TEXT = 'The order was just changed elsewhere; the list has been refreshed.'

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

// The milestones of the written repo before the optimistic edit, for the rollback on error.
interface RankSnapshot {
  milestones: PrioritiesData['repos'][number]['milestones'] | undefined
}

function rankErrorText(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409) return RANK_CONFLICT_TEXT
    return `Save failed: ${err.detail === '' ? `HTTP ${err.status}` : err.detail}`
  }
  return 'Save failed: server unreachable'
}

// Writes milestone ranks optimistically. The pending repos are tracked here, not through
// useMutation().isPending/variables, which only describe the latest call; two repos may be in
// flight at once. A repo enters the set synchronously in mutate, so a second click in the same
// tick is ignored, and leaves it only once its write and the refetch after it have landed.
// A failed write rolls back its own repo at once from the snapshot; the list is refetched once
// the last write has settled, so a refetch for one repo never wipes the optimistic ranks of
// another that is still in flight. A poll that lands in between can still show the old ranks
// of an in-flight repo for a moment, until the refetch after its write.
export function useMilestoneRanks() {
  const client = useQueryClient()
  const pendingRef = useRef(new Set<string>())
  const [pending, setPending] = useState<ReadonlySet<string>>(() => new Set())
  const [error, setError] = useState<string | null>(null)

  const release = useCallback((repo: string) => {
    pendingRef.current.delete(repo)
    setPending(new Set(pendingRef.current))
  }, [])

  const { mutate: run } = useMutation({
    mutationKey: milestoneRanksKey,
    mutationFn: ({ repo, changes }: RankVars) => saveMilestoneRanks(repo, changes),
    onMutate: async (vars): Promise<RankSnapshot> => {
      setError(null)
      await client.cancelQueries({ queryKey: queryKeys.priorities })
      const before = client.getQueryData<PrioritiesData>(queryKeys.priorities)
      const milestones = before?.repos.find((r) => r.slug === vars.repo)?.milestones
      if (before !== undefined) client.setQueryData<PrioritiesData>(queryKeys.priorities, withRanks(before, vars))
      return { milestones }
    },
    onError: (err, vars, snapshot) => {
      setError(rankErrorText(err))
      const milestones = snapshot?.milestones
      if (milestones === undefined) return
      client.setQueryData<PrioritiesData>(queryKeys.priorities, (old) =>
        old === undefined
          ? old
          : { ...old, repos: old.repos.map((r) => (r.slug === vars.repo ? { ...r, milestones } : r)) },
      )
    },
    onSettled: async (_data, _err, vars) => {
      try {
        // This mutation still counts as running here. When repo A fails while repo B is still
        // in flight, A is rolled back and released without a refetch, so a quick second click
        // on A may plan from stale ranks and get a 409; the refetch after that heals it.
        if (client.isMutating({ mutationKey: milestoneRanksKey }) === 1) {
          await client.invalidateQueries({ queryKey: queryKeys.priorities })
        }
      } finally {
        release(vars.repo)
      }
    },
  })

  const mutate = useCallback(
    (vars: RankVars) => {
      if (pendingRef.current.has(vars.repo)) return
      pendingRef.current.add(vars.repo)
      setPending(new Set(pendingRef.current))
      run(vars)
    },
    [run],
  )
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
