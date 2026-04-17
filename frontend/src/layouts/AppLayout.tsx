import { Layout } from 'antd';
import { Outlet } from 'react-router-dom';
import Sidebar from './Sidebar';
import AppHeader from './Header';

function RootLayout() {
  return (
    <Layout className="min-h-screen">
      <Sidebar />
      <Layout>
        <AppHeader />
        <Layout.Content className="p-4">
          <Outlet />
        </Layout.Content>
      </Layout>
    </Layout>
  );
}

export default RootLayout;