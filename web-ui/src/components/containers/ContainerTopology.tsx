'use client';

import { Box, Typography, Button, CircularProgress } from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import RefreshIcon from '@mui/icons-material/Refresh';
import { Container, ContainerMetricsWithRate } from '@/src/types/container';
import ContainerNode from './ContainerNode';

interface ContainerTopologyProps {
  containers: Container[];
  metricsMap: Record<string, ContainerMetricsWithRate>;
  isLoading: boolean;
  error?: Error | null;
  onCreateContainer: () => void;
  onDeleteContainer: (username: string) => void;
  onStartContainer: (username: string) => void;
  onStopContainer: (username: string) => void;
  onTerminalContainer?: (username: string) => void;
  onRefresh: () => void;
}

export default function ContainerTopology({
  containers,
  metricsMap,
  isLoading,
  error,
  onCreateContainer,
  onDeleteContainer,
  onStartContainer,
  onStopContainer,
  onTerminalContainer,
  onRefresh,
}: ContainerTopologyProps) {
  if (isLoading && containers.length === 0) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: 300 }}>
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return (
      <Box sx={{ p: 3, textAlign: 'center' }}>
        <Typography color="error" gutterBottom>
          Failed to load containers
        </Typography>
        <Typography variant="body2" color="text.secondary">
          {error.message}
        </Typography>
        <Button onClick={onRefresh} sx={{ mt: 2 }}>
          Retry
        </Button>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h5">
          Containers ({containers.length})
        </Typography>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            variant="outlined"
            startIcon={<RefreshIcon />}
            onClick={onRefresh}
            disabled={isLoading}
          >
            Refresh
          </Button>
          <Button
            variant="contained"
            startIcon={<AddIcon />}
            onClick={onCreateContainer}
          >
            Create Container
          </Button>
        </Box>
      </Box>

      {containers.length === 0 ? (
        <Box sx={{ textAlign: 'center', py: 6 }}>
          <Typography color="text.secondary" gutterBottom>
            No containers found
          </Typography>
          <Button
            variant="contained"
            startIcon={<AddIcon />}
            onClick={onCreateContainer}
            sx={{ mt: 2 }}
          >
            Create your first container
          </Button>
        </Box>
      ) : (
        <Box
          sx={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
            gap: 2,
          }}
        >
          {containers.map((container) => (
            <ContainerNode
              key={container.name}
              container={container}
              metrics={metricsMap[container.name]}
              onDelete={onDeleteContainer}
              onStart={onStartContainer}
              onStop={onStopContainer}
              onTerminal={onTerminalContainer}
            />
          ))}
        </Box>
      )}
    </Box>
  );
}
