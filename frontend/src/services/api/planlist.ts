import { api } from '@/services/api';
import type { ApiResponse, PaginatedResponse, PlanListCreate, PlanListDetailResponse, PlanListResponse } from '@/types/models';

export async function createPlanList(payload: PlanListCreate): Promise<PlanListResponse> {
  const response = await api.post<ApiResponse<PlanListResponse>>('/planlist', payload);
  return response.data;
}

export async function listPlanLists(limit = 20, offset = 0): Promise<PaginatedResponse<PlanListResponse>> {
  const response = await api.get<ApiResponse<PaginatedResponse<PlanListResponse>>>('/planlist', {
    params: { limit, offset },
  });
  return response.data;
}

export async function getPlanListById(planlistId: string): Promise<PlanListDetailResponse> {
  const response = await api.get<ApiResponse<PlanListDetailResponse>>(`/planlist/${planlistId}`);
  return response.data;
}

export async function listWaitingPlanLists(limit = 20, offset = 0): Promise<PaginatedResponse<PlanListResponse>> {
  const response = await api.get<ApiResponse<PaginatedResponse<PlanListResponse>>>('/planlist/waiting', {
    params: { limit, offset },
  });
  return response.data;
}