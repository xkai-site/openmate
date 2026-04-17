import { DashboardOutlined, OrderedListOutlined, ApartmentOutlined, NodeIndexOutlined } from '@ant-design/icons';
import { Layout, Menu } from 'antd';
import { Link, useLocation } from 'react-router-dom';
import { useUIStore } from '@/store';

const { Sider } = Layout;

const menuItems = [
  { key: '/aitree', icon: <NodeIndexOutlined />, label: <Link to="/aitree">AITree</Link> },
  { key: '/dashboard', icon: <DashboardOutlined />, label: <Link to="/dashboard">Dashboard</Link> },
  { key: '/planlist', icon: <OrderedListOutlined />, label: <Link to="/planlist">PlanList</Link> },
  { key: '/topic', icon: <ApartmentOutlined />, label: <Link to="/topic">Topic</Link> },
];

function Sidebar() {
  const location = useLocation();
  const collapsed = useUIStore((state) => state.sidebarCollapsed);

  return (
    <Sider collapsible trigger={null} collapsed={collapsed} width={220} theme="light" style={{ borderRight: '1px solid #f0f0f0' }}>
      <div className="h-14 flex items-center px-4 text-base font-semibold text-slate-800">AITQ Console</div>
      <Menu mode="inline" selectedKeys={[location.pathname]} items={menuItems} />
    </Sider>
  );
}

export default Sidebar;