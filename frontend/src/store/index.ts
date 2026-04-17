import { create } from 'zustand';

interface UIState {
  sidebarCollapsed: boolean;
  activeTopicId: string | null;
  setActiveTopicId: (topicId: string | null) => void;
  toggleSidebar: () => void;
}

export const useUIStore = create<UIState>((set) => ({
  sidebarCollapsed: false,
  activeTopicId: null,
  setActiveTopicId: (topicId) => set({ activeTopicId: topicId }),
  toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
}));