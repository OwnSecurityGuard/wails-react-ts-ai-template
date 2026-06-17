import React from 'react';
import { CopilotKit } from '@copilotkit/react-core/v2';
import '@copilotkit/react-core/v2/styles.css';

const RUNTIME_URL = 'http://localhost:18999/api/copilotkit';

interface CopilotKitProviderProps {
  children: React.ReactNode;
}

/**
 * CopilotKit Provider 包装器
 * 配置前端连接到 Golang 后端实现的 AG-UI Runtime
 */
const CopilotKitProvider: React.FC<CopilotKitProviderProps> = ({ children }) => {
  return (
    <CopilotKit
      runtimeUrl={RUNTIME_URL}
    >
      {children}
    </CopilotKit>
  );
};

export default CopilotKitProvider;
