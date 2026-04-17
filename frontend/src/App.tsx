import { Suspense, lazy } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { Spin } from 'antd';

const HomePage = lazy(() => import('@/pages/Home/index.tsx'));
const AITreePage = lazy(() => import('@/pages/AITree/index.tsx'));
const WorkspacePage = lazy(() => import('@/pages/Workspace/index.tsx'));
const PlanListPage = lazy(() => import('@/pages/PlanList/index.tsx'));
const TopicPage = lazy(() => import('@/pages/Topic/index.tsx'));
const DashboardPage = lazy(() => import('@/pages/Dashboard/index.tsx'));

function App() {
  return (
    <Suspense fallback={<div className="h-screen w-full flex items-center justify-center bg-slate-900"><Spin size="large" /></div>}>
      <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/aitree" element={<AITreePage />} />
        <Route path="/workspace/:nodeId" element={<WorkspacePage />} />
        <Route path="/dashboard" element={<DashboardPage />} />
        <Route path="/planlist" element={<PlanListPage />} />
        <Route path="/topic" element={<TopicPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Suspense>
  );
}

export default App;