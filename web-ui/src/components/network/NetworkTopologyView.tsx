'use client';

import { useState } from 'react';
import {
  Box,
  Typography,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Chip,
  CircularProgress,
  Button,
  Switch,
  FormControlLabel,
  IconButton,
  Tooltip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Link,
  Autocomplete,
  MenuItem,
  Select,
  FormControl,
  InputLabel,
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import CloudIcon from '@mui/icons-material/Cloud';
import RouterIcon from '@mui/icons-material/Router';
import DnsIcon from '@mui/icons-material/Dns';
import BlockIcon from '@mui/icons-material/Block';
import AddIcon from '@mui/icons-material/Add';
import DeleteIcon from '@mui/icons-material/Delete';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { NetworkTopology, ProxyRoute, NetworkNode, DNSRecord } from '@/src/types/app';

interface NetworkTopologyViewProps {
  topology: NetworkTopology;
  routes: ProxyRoute[];
  dnsRecords?: DNSRecord[];
  baseDomain?: string;
  isLoading: boolean;
  error?: Error | null;
  includeStopped: boolean;
  onIncludeStoppedChange: (value: boolean) => void;
  onAddRoute?: (domain: string, targetIp: string, targetPort: number) => Promise<void>;
  onDeleteRoute?: (domain: string) => Promise<void>;
  onRefresh: () => void;
}

// Simple visualization component for the network
function NetworkDiagram({ topology }: { topology: NetworkTopology }) {
  const proxyNode = topology.nodes.find(n => n.type === 'proxy');
  const containerNodes = topology.nodes.filter(n => n.type === 'container');

  return (
    <Box sx={{ p: 3, textAlign: 'center' }}>
      {/* Internet */}
      <Box sx={{ mb: 2 }}>
        <CloudIcon sx={{ fontSize: 40, color: 'primary.main' }} />
        <Typography variant="body2" color="text.secondary">
          Internet
        </Typography>
      </Box>

      {/* Arrow */}
      <Box sx={{ height: 30, borderLeft: '2px solid', borderColor: 'grey.400', width: 0, mx: 'auto' }} />

      {/* Proxy */}
      {proxyNode && (
        <Paper
          sx={{
            p: 2,
            display: 'inline-block',
            minWidth: 200,
            mb: 2,
            bgcolor: 'primary.light',
            color: 'primary.contrastText',
          }}
        >
          <RouterIcon sx={{ fontSize: 30 }} />
          <Typography variant="subtitle1">{proxyNode.name}</Typography>
          <Typography variant="body2">{proxyNode.ipAddress}</Typography>
          <Chip label="TLS Termination" size="small" sx={{ mt: 1 }} />
        </Paper>
      )}

      {/* Connections */}
      {containerNodes.length > 0 && (
        <>
          <Box
            sx={{
              display: 'flex',
              justifyContent: 'center',
              gap: 2,
              my: 2,
            }}
          >
            {containerNodes.slice(0, 5).map((node, idx) => (
              <Box key={node.id} sx={{ height: 30, borderLeft: '2px solid', borderColor: 'grey.400', width: 0 }} />
            ))}
          </Box>

          {/* Container Nodes */}
          <Box
            sx={{
              display: 'flex',
              flexWrap: 'wrap',
              justifyContent: 'center',
              gap: 2,
            }}
          >
            {containerNodes.map((node) => (
              <ContainerNodeCard key={node.id} node={node} />
            ))}
          </Box>
        </>
      )}

      {/* Network Info */}
      <Box sx={{ mt: 3, p: 2, bgcolor: 'grey.100', borderRadius: 1 }}>
        <Typography variant="body2" color="text.secondary">
          Network: {topology.networkCidr} | Gateway: {topology.gatewayIp}
        </Typography>
      </Box>
    </Box>
  );
}

function ContainerNodeCard({ node }: { node: NetworkNode }) {
  const isRunning = node.state === 'running';

  return (
    <Paper
      sx={{
        p: 2,
        minWidth: 150,
        borderLeft: 4,
        borderColor: isRunning ? 'success.main' : 'grey.400',
        opacity: isRunning ? 1 : 0.7,
      }}
    >
      <DnsIcon sx={{ fontSize: 24, color: isRunning ? 'success.main' : 'grey.500' }} />
      <Typography variant="subtitle2" noWrap sx={{ maxWidth: 130 }}>
        {node.name}
      </Typography>
      <Typography variant="caption" color="text.secondary" display="block">
        {node.ipAddress || 'No IP'}
      </Typography>
      {node.aclName && (
        <Chip
          icon={<BlockIcon sx={{ fontSize: 14 }} />}
          label={node.aclName.replace('acl-', '')}
          size="small"
          variant="outlined"
          sx={{ mt: 0.5, fontSize: 10 }}
        />
      )}
    </Paper>
  );
}

// Route Table Component
function RouteTable({ routes, onDelete }: { routes: ProxyRoute[]; onDelete?: (domain: string) => void }) {
  if (routes.length === 0) {
    return (
      <Box sx={{ textAlign: 'center', py: 4 }}>
        <Typography color="text.secondary">No routes configured</Typography>
      </Box>
    );
  }

  return (
    <TableContainer>
      <Table size="small">
        <TableHead>
          <TableRow>
            <TableCell>Domain</TableCell>
            <TableCell>Target</TableCell>
            <TableCell>App</TableCell>
            <TableCell>Status</TableCell>
            <TableCell align="right">Actions</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {routes.map((route) => (
            <TableRow key={route.fullDomain || route.subdomain}>
              <TableCell>
                <Link
                  href={`https://${route.fullDomain}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}
                >
                  {route.fullDomain}
                  <OpenInNewIcon sx={{ fontSize: 14 }} />
                </Link>
              </TableCell>
              <TableCell>
                <Typography variant="body2" fontFamily="monospace">
                  {route.containerIp ? `${route.containerIp}:${route.port}` : 'N/A'}
                </Typography>
              </TableCell>
              <TableCell>
                <Typography variant="body2" color="text.secondary">
                  {route.appName || '-'}
                </Typography>
              </TableCell>
              <TableCell>
                <Chip
                  label={route.active ? 'Active' : 'Inactive'}
                  color={route.active ? 'success' : 'default'}
                  size="small"
                />
              </TableCell>
              <TableCell align="right">
                {onDelete && (
                  <Tooltip title="Delete route">
                    <IconButton
                      size="small"
                      color="error"
                      onClick={() => onDelete(route.fullDomain)}
                    >
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

export default function NetworkTopologyView({
  topology,
  routes,
  dnsRecords = [],
  baseDomain = '',
  isLoading,
  error,
  includeStopped,
  onIncludeStoppedChange,
  onAddRoute,
  onDeleteRoute,
  onRefresh,
}: NetworkTopologyViewProps) {
  // Dialog states
  const [addRouteDialog, setAddRouteDialog] = useState(false);
  const [newRoute, setNewRoute] = useState({ domain: '', targetIp: '', targetPort: '' });
  const [deleteRouteDialog, setDeleteRouteDialog] = useState<{ open: boolean; domain: string }>({
    open: false,
    domain: '',
  });

  // Build domain suggestions from DNS records
  // Each record has: name (subdomain like "pes"), data (full domain like "pes.kafeido.app")
  const domainSuggestions = dnsRecords.map(r => ({
    subdomain: r.name,
    fullDomain: r.data,
  }));

  // Also add existing route domains if not already in suggestions
  const existingDomains = routes.map(r => r.fullDomain).filter(Boolean);
  existingDomains.forEach(domain => {
    if (!domainSuggestions.find(s => s.fullDomain === domain)) {
      const subdomain = domain.replace('.' + baseDomain, '');
      domainSuggestions.push({ subdomain, fullDomain: domain });
    }
  });

  // Extract container options from topology nodes
  const containerOptions = topology.nodes
    .filter(node => node.type === 'container' && node.ipAddress && node.state === 'running')
    .map(node => ({
      name: node.name,
      ip: node.ipAddress || '',
    }));

  const handleAddRoute = async () => {
    if (onAddRoute && newRoute.domain && newRoute.targetIp && newRoute.targetPort) {
      await onAddRoute(newRoute.domain, newRoute.targetIp, parseInt(newRoute.targetPort, 10));
      setAddRouteDialog(false);
      setNewRoute({ domain: '', targetIp: '', targetPort: '' });
    }
  };

  const handleDeleteRoute = (domain: string) => {
    setDeleteRouteDialog({ open: true, domain });
  };

  const handleConfirmDeleteRoute = async () => {
    if (onDeleteRoute) {
      await onDeleteRoute(deleteRouteDialog.domain);
      setDeleteRouteDialog({ open: false, domain: '' });
    }
  };

  if (isLoading && topology.nodes.length === 0) {
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
          Failed to load network topology
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
      {/* Header */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
        <Typography variant="h5">Network Topology</Typography>
        <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
          <FormControlLabel
            control={
              <Switch
                checked={includeStopped}
                onChange={(e) => onIncludeStoppedChange(e.target.checked)}
              />
            }
            label="Include stopped"
          />
          <Button
            variant="outlined"
            startIcon={<RefreshIcon />}
            onClick={onRefresh}
            disabled={isLoading}
          >
            Refresh
          </Button>
        </Box>
      </Box>

      {/* Network Diagram */}
      <Paper sx={{ mb: 3 }}>
        <NetworkDiagram topology={topology} />
      </Paper>

      {/* Route Table */}
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mt: 4, mb: 2 }}>
        <Typography variant="h6">
          Proxy Routes ({routes.length})
        </Typography>
        {onAddRoute && (
          <Button
            variant="outlined"
            startIcon={<AddIcon />}
            onClick={() => setAddRouteDialog(true)}
          >
            Add Route
          </Button>
        )}
      </Box>
      <Paper>
        <RouteTable routes={routes} onDelete={onDeleteRoute ? handleDeleteRoute : undefined} />
      </Paper>

      {/* Add Route Dialog */}
      <Dialog open={addRouteDialog} onClose={() => setAddRouteDialog(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Add Proxy Route</DialogTitle>
        <DialogContent>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            Create a new proxy route to map a domain to a container IP and port.
          </Typography>

          {/* Domain - Autocomplete with suggestions from DNS records */}
          <Autocomplete
            freeSolo
            options={domainSuggestions}
            getOptionLabel={(option) => {
              if (typeof option === 'string') return option;
              return option.fullDomain;
            }}
            value={newRoute.domain}
            onChange={(_, value) => {
              if (typeof value === 'string') {
                setNewRoute({ ...newRoute, domain: value });
              } else if (value) {
                setNewRoute({ ...newRoute, domain: value.fullDomain });
              }
            }}
            onInputChange={(_, value) => setNewRoute({ ...newRoute, domain: value })}
            renderOption={(props, option) => (
              <li {...props} key={typeof option === 'string' ? option : option.fullDomain}>
                <Box>
                  <Typography variant="body2" fontWeight={500}>
                    {typeof option === 'string' ? option : option.subdomain}
                  </Typography>
                  {typeof option !== 'string' && (
                    <Typography variant="caption" color="text.secondary">
                      {option.fullDomain}
                    </Typography>
                  )}
                </Box>
              </li>
            )}
            renderInput={(params) => (
              <TextField
                {...params}
                fullWidth
                label="Domain"
                placeholder={baseDomain ? `subdomain.${baseDomain}` : 'test.example.com'}
                helperText={baseDomain ? `Base domain: ${baseDomain}` : 'Enter the full domain name'}
                sx={{ mb: 2 }}
              />
            )}
          />

          {/* Target - Select from containers or custom input */}
          <Autocomplete
            freeSolo
            options={containerOptions}
            getOptionLabel={(option) => {
              if (typeof option === 'string') return option;
              return `${option.name} (${option.ip})`;
            }}
            value={newRoute.targetIp}
            onChange={(_, value) => {
              if (typeof value === 'string') {
                setNewRoute({ ...newRoute, targetIp: value });
              } else if (value) {
                setNewRoute({ ...newRoute, targetIp: value.ip });
              }
            }}
            onInputChange={(_, value) => {
              // Only update if it looks like an IP or the field is being cleared
              if (!value || value.match(/^[\d.]+$/) || value.includes('(')) {
                const ipMatch = value.match(/\(([^)]+)\)/);
                if (ipMatch) {
                  setNewRoute({ ...newRoute, targetIp: ipMatch[1] });
                } else {
                  setNewRoute({ ...newRoute, targetIp: value });
                }
              }
            }}
            renderOption={(props, option) => (
              <li {...props} key={typeof option === 'string' ? option : option.ip}>
                <Box>
                  <Typography variant="body2" fontWeight={500}>
                    {typeof option === 'string' ? option : option.name}
                  </Typography>
                  {typeof option !== 'string' && (
                    <Typography variant="caption" color="text.secondary">
                      {option.ip}
                    </Typography>
                  )}
                </Box>
              </li>
            )}
            renderInput={(params) => (
              <TextField
                {...params}
                fullWidth
                label="Target IP"
                placeholder="10.0.3.136"
                helperText="Select a container or enter IP manually"
                sx={{ mb: 2 }}
              />
            )}
          />

          <TextField
            fullWidth
            label="Target Port"
            placeholder="8080"
            type="number"
            value={newRoute.targetPort}
            onChange={(e) => setNewRoute({ ...newRoute, targetPort: e.target.value })}
            helperText="The port on the container"
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAddRouteDialog(false)}>Cancel</Button>
          <Button
            onClick={handleAddRoute}
            variant="contained"
            disabled={!newRoute.domain || !newRoute.targetIp || !newRoute.targetPort}
          >
            Add Route
          </Button>
        </DialogActions>
      </Dialog>

      {/* Delete Route Confirmation Dialog */}
      <Dialog open={deleteRouteDialog.open} onClose={() => setDeleteRouteDialog({ open: false, domain: '' })}>
        <DialogTitle>Delete Proxy Route</DialogTitle>
        <DialogContent>
          <Typography gutterBottom>
            Are you sure you want to delete the route for <strong>{deleteRouteDialog.domain}</strong>?
          </Typography>
          <Typography variant="body2" color="text.secondary">
            This will remove the proxy configuration for this domain.
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteRouteDialog({ open: false, domain: '' })}>
            Cancel
          </Button>
          <Button onClick={handleConfirmDeleteRoute} color="error">
            Delete
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
