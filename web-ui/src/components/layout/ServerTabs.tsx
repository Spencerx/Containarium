'use client';

import { Tabs, Tab, Box } from '@mui/material';
import CloseIcon from '@mui/icons-material/Close';
import EditIcon from '@mui/icons-material/Edit';
import CircleIcon from '@mui/icons-material/Circle';
import { ServerWithStatus } from '@/src/types/server';

interface ServerTabsProps {
  servers: ServerWithStatus[];
  activeServerId: string | null;
  onServerChange: (serverId: string) => void;
  onRemoveServer: (serverId: string) => void;
  onEditServer: (serverId: string) => void;
}

function getStatusColor(status: ServerWithStatus['status']): 'success' | 'error' | 'warning' | 'default' {
  switch (status) {
    case 'connected':
      return 'success';
    case 'error':
      return 'error';
    case 'connecting':
      return 'warning';
    default:
      return 'default';
  }
}

export default function ServerTabs({ servers, activeServerId, onServerChange, onRemoveServer, onEditServer }: ServerTabsProps) {
  if (servers.length === 0) {
    return null;
  }

  const handleChange = (_event: React.SyntheticEvent, newValue: string) => {
    onServerChange(newValue);
  };

  return (
    <Box sx={{ borderBottom: 1, borderColor: 'divider', bgcolor: 'background.paper' }}>
      <Tabs
        value={activeServerId || false}
        onChange={handleChange}
        variant="scrollable"
        scrollButtons="auto"
      >
        {servers.map((server) => (
          <Tab
            key={server.id}
            value={server.id}
            label={
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <CircleIcon
                  sx={{
                    fontSize: 10,
                    color: getStatusColor(server.status) === 'success' ? 'success.main' :
                           getStatusColor(server.status) === 'error' ? 'error.main' :
                           getStatusColor(server.status) === 'warning' ? 'warning.main' :
                           'grey.500'
                  }}
                />
                <span>{server.name}</span>
                <Box
                  component="span"
                  onClick={(e) => {
                    e.stopPropagation();
                    onEditServer(server.id);
                  }}
                  sx={{
                    ml: 0.5,
                    p: 0.25,
                    display: 'inline-flex',
                    cursor: 'pointer',
                    borderRadius: '50%',
                    '&:hover': { bgcolor: 'action.hover' },
                  }}
                  title="Edit server"
                >
                  <EditIcon sx={{ fontSize: 16 }} />
                </Box>
                <Box
                  component="span"
                  onClick={(e) => {
                    e.stopPropagation();
                    onRemoveServer(server.id);
                  }}
                  sx={{
                    p: 0.25,
                    display: 'inline-flex',
                    cursor: 'pointer',
                    borderRadius: '50%',
                    '&:hover': { bgcolor: 'action.hover' },
                  }}
                  title="Remove server"
                >
                  <CloseIcon sx={{ fontSize: 16 }} />
                </Box>
              </Box>
            }
            sx={{ minHeight: 48 }}
          />
        ))}
      </Tabs>
    </Box>
  );
}
