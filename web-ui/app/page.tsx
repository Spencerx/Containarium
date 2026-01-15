'use client';

import { useState, useMemo } from 'react';
import dynamic from 'next/dynamic';
import { Box, Typography, CircularProgress } from '@mui/material';
import AppBar from '@/src/components/layout/AppBar';
import ServerTabs from '@/src/components/layout/ServerTabs';
import AddServerDialog from '@/src/components/servers/AddServerDialog';
import ContainerTopology from '@/src/components/containers/ContainerTopology';
import CreateContainerDialog from '@/src/components/containers/CreateContainerDialog';
import DeleteConfirmDialog from '@/src/components/containers/DeleteConfirmDialog';
import { useServers } from '@/src/lib/hooks/useServers';
import { useContainers, CreateContainerProgress } from '@/src/lib/hooks/useContainers';
import { useMetrics } from '@/src/lib/hooks/useMetrics';
import { CreateContainerRequest, ContainerMetricsWithRate } from '@/src/types/container';
import { Server } from '@/src/types/server';

// Dynamic import for TerminalDialog to avoid SSR issues with xterm
const TerminalDialog = dynamic(
  () => import('@/src/components/containers/TerminalDialog'),
  { ssr: false }
);

export default function Home() {
  const {
    servers,
    activeServer,
    activeServerId,
    setActiveServerId,
    addServer,
    removeServer,
    updateServer,
    isLoading: serversLoading,
  } = useServers();

  const {
    containers,
    isLoading: containersLoading,
    error: containersError,
    createContainer,
    deleteContainer,
    startContainer,
    stopContainer,
    refresh,
  } = useContainers(activeServer);

  // Fetch metrics for all containers (only when there's an active server with running containers)
  const hasRunningContainers = containers.some(c => c.state === 'Running');
  const { metrics } = useMetrics(activeServer, hasRunningContainers);

  // Convert metrics array to a map by container name for easy lookup
  const metricsMap = useMemo(() => {
    const map: Record<string, ContainerMetricsWithRate> = {};
    for (const m of metrics) {
      map[m.name] = m;
    }
    return map;
  }, [metrics]);

  const [serverDialogOpen, setServerDialogOpen] = useState(false);
  const [editingServer, setEditingServer] = useState<Server | null>(null);
  const [createContainerOpen, setCreateContainerOpen] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<{ open: boolean; containerName: string }>({
    open: false,
    containerName: '',
  });
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [terminalUsername, setTerminalUsername] = useState('');

  const handleAddServer = async (name: string, endpoint: string, token: string) => {
    await addServer(name, endpoint, token);
  };

  const handleUpdateServer = async (serverId: string, name: string, endpoint: string, token: string) => {
    await updateServer(serverId, name, endpoint, token);
  };

  const handleEditServer = (serverId: string) => {
    const server = servers.find(s => s.id === serverId);
    if (server) {
      setEditingServer(server);
      setServerDialogOpen(true);
    }
  };

  const handleCloseServerDialog = () => {
    setServerDialogOpen(false);
    setEditingServer(null);
  };

  const handleCreateContainer = async (
    request: CreateContainerRequest,
    onProgress?: (progress: CreateContainerProgress) => void
  ) => {
    await createContainer(request, onProgress);
  };

  const handleDeleteContainer = (username: string) => {
    setDeleteConfirm({ open: true, containerName: username });
  };

  const handleConfirmDelete = async () => {
    await deleteContainer(deleteConfirm.containerName, true);
  };

  const handleOpenTerminal = (username: string) => {
    console.log('handleOpenTerminal called:', username);
    console.log('activeServer:', activeServer?.endpoint);
    setTerminalUsername(username);
    setTerminalOpen(true);
  };

  const handleCloseTerminal = () => {
    setTerminalOpen(false);
    setTerminalUsername('');
  };

  if (serversLoading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', minHeight: '100vh' }}>
      <AppBar onAddServer={() => setServerDialogOpen(true)} />

      <ServerTabs
        servers={servers}
        activeServerId={activeServerId}
        onServerChange={setActiveServerId}
        onRemoveServer={removeServer}
        onEditServer={handleEditServer}
      />

      {servers.length === 0 ? (
        <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', justifyContent: 'center', alignItems: 'center' }}>
          <Typography variant="h5" color="text.secondary" gutterBottom>
            No servers added
          </Typography>
          <Typography color="text.secondary">
            Click "Add Server" to connect to a Containarium server
          </Typography>
        </Box>
      ) : activeServer ? (
        <ContainerTopology
          containers={containers}
          metricsMap={metricsMap}
          isLoading={containersLoading}
          error={containersError as Error | null}
          onCreateContainer={() => setCreateContainerOpen(true)}
          onDeleteContainer={handleDeleteContainer}
          onStartContainer={startContainer}
          onStopContainer={stopContainer}
          onTerminalContainer={handleOpenTerminal}
          onRefresh={refresh}
        />
      ) : (
        <Box sx={{ flex: 1, display: 'flex', justifyContent: 'center', alignItems: 'center' }}>
          <Typography color="text.secondary">
            Select a server to view containers
          </Typography>
        </Box>
      )}

      <AddServerDialog
        open={serverDialogOpen}
        onClose={handleCloseServerDialog}
        onAdd={handleAddServer}
        onUpdate={handleUpdateServer}
        editServer={editingServer}
      />

      <CreateContainerDialog
        open={createContainerOpen}
        onClose={() => setCreateContainerOpen(false)}
        onSubmit={handleCreateContainer}
      />

      <DeleteConfirmDialog
        open={deleteConfirm.open}
        containerName={deleteConfirm.containerName}
        onClose={() => setDeleteConfirm({ open: false, containerName: '' })}
        onConfirm={handleConfirmDelete}
      />

      {activeServer && terminalOpen && (
        <TerminalDialog
          open={terminalOpen}
          onClose={handleCloseTerminal}
          containerName={terminalUsername + '-container'}
          username={terminalUsername}
          serverEndpoint={activeServer.endpoint}
          token={activeServer.token}
        />
      )}
    </Box>
  );
}
