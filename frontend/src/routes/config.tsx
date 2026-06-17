import React from 'react';
import {
  HomeOutlined,
  KeyOutlined,
  VideoCameraOutlined,
  SettingOutlined,
  InfoCircleOutlined,
  RobotOutlined,
} from '@ant-design/icons';
import Home from '../pages/Home';
import Settings from '../pages/Settings';
import KeySimulation from '../pages/KeySimulation';
import MacroEditor from '../pages/MacroEditor';
import ModeSettings from '../pages/ModeSettings';
import About from '../pages/About';
import AIChat from '../pages/AIChat';
import AISettings from '../pages/AISettings';

export interface RouteConfig {
  path: string;
  element: React.ReactNode;
  icon?: React.ReactNode;
  label?: string;
}

export const routes: RouteConfig[] = [
  {
    path: '/home',
    element: <Home />,
    icon: <HomeOutlined />,
    label: 'menu.home'
  },
  {
    path: '/ai-chat',
    element: <AIChat />,
    icon: <RobotOutlined />,
    label: 'menu.aiChat'
  },
  {
    path: '/key-simulation',
    element: <KeySimulation />,
    icon: <KeyOutlined />,
    label: 'menu.keySimulation'
  },
  {
    path: '/macro-editor',
    element: <MacroEditor />,
    icon: <VideoCameraOutlined />,
    label: 'menu.macroEditor'
  },
  {
    path: '/mode-settings',
    element: <ModeSettings />,
    icon: <SettingOutlined />,
    label: 'menu.modeSettings'
  },
  {
    path: '/about',
    element: <About />,
    icon: <InfoCircleOutlined />,
    label: 'menu.about'
  },
  {
    path: '/settings',
    element: <Settings />,
    icon: <SettingOutlined />,
    label: 'menu.settings'
  },
  {
    path: '/ai-settings',
    element: <AISettings />,
    label: 'menu.aiSettings'
  }
];
