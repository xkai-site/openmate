import { useMemo } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createPlanList, listPlanLists, listWaitingPlanLists } from '@/services/api/planlist';
import type { PlanListCreate } from '@/types/models';
import { usePagination } from './usePagination';

export function usePlanList() {
  const queryClient = useQueryClient();

  const allPagination = usePagination(10);
  const waitingPagination = usePagination(10);

  const allQuery = useQuery({
    queryKey: ['planlist', 'all', allPagination.pageSize, allPagination.offset],
    queryFn: () => listPlanLists(allPagination.pageSize, allPagination.offset),
  });

  const waitingQuery = useQuery({
    queryKey: ['planlist', 'waiting', waitingPagination.pageSize, waitingPagination.offset],
    queryFn: () => listWaitingPlanLists(waitingPagination.pageSize, waitingPagination.offset),
  });

  const createMutation = useMutation({
    mutationFn: (payload: PlanListCreate) => createPlanList(payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['planlist'] });
    },
  });

  const allItems = useMemo(() => allQuery.data?.items ?? [], [allQuery.data]);
  const waitingItems = useMemo(() => waitingQuery.data?.items ?? [], [waitingQuery.data]);

  return {
    allPagination,
    waitingPagination,
    allQuery,
    waitingQuery,
    allItems,
    waitingItems,
    createMutation,
  };
}