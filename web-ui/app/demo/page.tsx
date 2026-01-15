'use client';

import { Box, Typography } from '@mui/material';
import AppBar from '@/src/components/layout/AppBar';
import ContainerTopology from '@/src/components/containers/ContainerTopology';
import { Container, ContainerMetricsWithRate } from '@/src/types/container';

// Mock containers with varied states and resources
const mockContainers: Container[] = [
  {
    name: 'alice-container',
    username: 'alice',
    state: 'Running',
    ipAddress: '10.0.100.12',
    cpu: '8',
    memory: '16GB',
    disk: '100GB',
    gpu: 'NVIDIA RTX 4090',
    image: 'ubuntu:24.04',
    dockerEnabled: true,
    createdAt: '2025-01-10T08:30:00Z',
    updatedAt: '2025-01-15T10:00:00Z',
    labels: { team: 'ml-research' },
    sshKeys: [],
  },
  {
    name: 'bob-container',
    username: 'bob',
    state: 'Running',
    ipAddress: '10.0.100.15',
    cpu: '4',
    memory: '8GB',
    disk: '50GB',
    gpu: '',
    image: 'ubuntu:22.04',
    dockerEnabled: true,
    createdAt: '2025-01-12T14:20:00Z',
    updatedAt: '2025-01-15T09:45:00Z',
    labels: { team: 'backend' },
    sshKeys: [],
  },
  {
    name: 'charlie-container',
    username: 'charlie',
    state: 'Running',
    ipAddress: '10.0.100.18',
    cpu: '16',
    memory: '32GB',
    disk: '200GB',
    gpu: 'NVIDIA A100',
    image: 'ubuntu:24.04',
    dockerEnabled: true,
    createdAt: '2025-01-08T11:00:00Z',
    updatedAt: '2025-01-15T10:15:00Z',
    labels: { team: 'ml-training' },
    sshKeys: [],
  },
  {
    name: 'david-container',
    username: 'david',
    state: 'Stopped',
    ipAddress: '',
    cpu: '2',
    memory: '4GB',
    disk: '30GB',
    gpu: '',
    image: 'ubuntu:22.04',
    dockerEnabled: false,
    createdAt: '2025-01-05T09:00:00Z',
    updatedAt: '2025-01-14T18:00:00Z',
    labels: { team: 'frontend' },
    sshKeys: [],
  },
  {
    name: 'emma-container',
    username: 'emma',
    state: 'Running',
    ipAddress: '10.0.100.22',
    cpu: '4',
    memory: '8GB',
    disk: '50GB',
    gpu: '',
    image: 'debian:12',
    dockerEnabled: true,
    createdAt: '2025-01-11T16:30:00Z',
    updatedAt: '2025-01-15T08:20:00Z',
    labels: { team: 'devops' },
    sshKeys: [],
  },
  {
    name: 'frank-container',
    username: 'frank',
    state: 'Creating',
    ipAddress: '',
    cpu: '8',
    memory: '16GB',
    disk: '100GB',
    gpu: 'NVIDIA RTX 3090',
    image: 'ubuntu:24.04',
    dockerEnabled: true,
    createdAt: '2025-01-15T10:25:00Z',
    updatedAt: '2025-01-15T10:25:00Z',
    labels: { team: 'ml-research' },
    sshKeys: [],
  },
];

// Mock metrics with varied usage levels
const mockMetricsMap: Record<string, ContainerMetricsWithRate> = {
  'alice-container': {
    name: 'alice-container',
    cpuUsageSeconds: 45000,
    cpuUsagePercent: 320, // 320% = using 3.2 of 8 cores (40% bar)
    memoryUsageBytes: 12 * 1024 * 1024 * 1024, // 12GB of 16GB (75%)
    memoryPeakBytes: 14 * 1024 * 1024 * 1024,
    diskUsageBytes: 65 * 1024 * 1024 * 1024, // 65GB of 100GB (65%)
    networkRxBytes: 2.5 * 1024 * 1024 * 1024,
    networkTxBytes: 1.2 * 1024 * 1024 * 1024,
    processCount: 156,
  },
  'bob-container': {
    name: 'bob-container',
    cpuUsageSeconds: 12000,
    cpuUsagePercent: 85, // 85% = low usage of 4 cores (21% bar)
    memoryUsageBytes: 3.2 * 1024 * 1024 * 1024, // 3.2GB of 8GB (40%)
    memoryPeakBytes: 5 * 1024 * 1024 * 1024,
    diskUsageBytes: 22 * 1024 * 1024 * 1024, // 22GB of 50GB (44%)
    networkRxBytes: 850 * 1024 * 1024,
    networkTxBytes: 320 * 1024 * 1024,
    processCount: 42,
  },
  'charlie-container': {
    name: 'charlie-container',
    cpuUsageSeconds: 180000,
    cpuUsagePercent: 1450, // 1450% = using 14.5 of 16 cores (90% bar - high!)
    memoryUsageBytes: 28 * 1024 * 1024 * 1024, // 28GB of 32GB (87.5% - high!)
    memoryPeakBytes: 30 * 1024 * 1024 * 1024,
    diskUsageBytes: 145 * 1024 * 1024 * 1024, // 145GB of 200GB (72.5%)
    networkRxBytes: 15 * 1024 * 1024 * 1024,
    networkTxBytes: 8 * 1024 * 1024 * 1024,
    processCount: 312,
  },
  'emma-container': {
    name: 'emma-container',
    cpuUsageSeconds: 8500,
    cpuUsagePercent: 45, // 45% = very low (11% bar)
    memoryUsageBytes: 1.8 * 1024 * 1024 * 1024, // 1.8GB of 8GB (22.5%)
    memoryPeakBytes: 3 * 1024 * 1024 * 1024,
    diskUsageBytes: 8 * 1024 * 1024 * 1024, // 8GB of 50GB (16%)
    networkRxBytes: 120 * 1024 * 1024,
    networkTxBytes: 45 * 1024 * 1024,
    processCount: 28,
  },
};

export default function DemoPage() {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', minHeight: '100vh' }}>
      <AppBar onAddServer={() => {}} />

      <Box sx={{ bgcolor: 'primary.main', color: 'white', py: 1, px: 2 }}>
        <Typography variant="body2">
          Demo Mode - Showing mock data for UI preview
        </Typography>
      </Box>

      <Box sx={{ borderBottom: 1, borderColor: 'divider', px: 2, py: 1, bgcolor: 'grey.50' }}>
        <Typography variant="body1" sx={{ fontWeight: 500 }}>
          GPU Cluster (demo-server.local)
        </Typography>
      </Box>

      <ContainerTopology
        containers={mockContainers}
        metricsMap={mockMetricsMap}
        isLoading={false}
        error={null}
        onCreateContainer={() => {}}
        onDeleteContainer={() => {}}
        onStartContainer={() => {}}
        onStopContainer={() => {}}
        onTerminalContainer={() => {}}
        onRefresh={() => {}}
      />
    </Box>
  );
}
