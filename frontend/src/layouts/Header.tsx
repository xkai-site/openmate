import { MenuFoldOutlined, MenuUnfoldOutlined } from '@ant-design/icons';
import { Button, Layout, Typography } from 'antd';
import { useUIStore } from '@/store';

const { Header } = Layout;

function AppHeader() {
  const sidebarCollapsed = useUIStore((state) => state.sidebarCollapsed);
  const toggleSidebar = useUIStore((state) => state.toggleSidebar);

  return (
    <Header className="!bg-white !px-4 !h-14 flex items-center justify-between border-b border-slate-100">
      <div className="flex items-center gap-3">
        <Button
          type="text"
          icon={sidebarCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
          onClick={toggleSidebar}
        />
        <Typography.Text className="!text-slate-700">AI Task Queue</Typography.Text>
      </div>
    </Header>
  );
}

export default AppHeader;