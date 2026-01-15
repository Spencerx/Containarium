'use client';

import { useState, useEffect } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Button,
  Alert,
  CircularProgress,
  Box,
} from '@mui/material';
import { getClient } from '@/src/lib/api/client';
import { Server } from '@/src/types/server';

interface AddServerDialogProps {
  open: boolean;
  onClose: () => void;
  onAdd: (name: string, endpoint: string, token: string) => Promise<void>;
  onUpdate?: (serverId: string, name: string, endpoint: string, token: string) => Promise<void>;
  editServer?: Server | null;
}

export default function AddServerDialog({ open, onClose, onAdd, onUpdate, editServer }: AddServerDialogProps) {
  const [name, setName] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [token, setToken] = useState('');
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<'success' | 'error' | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const isEditMode = !!editServer;

  // Populate form when editing
  useEffect(() => {
    if (editServer) {
      setName(editServer.name);
      setEndpoint(editServer.endpoint);
      setToken(editServer.token);
    }
  }, [editServer]);

  const resetForm = () => {
    setName('');
    setEndpoint('');
    setToken('');
    setTesting(false);
    setTestResult(null);
    setError(null);
    setSubmitting(false);
  };

  const handleClose = () => {
    resetForm();
    onClose();
  };

  const handleTestConnection = async () => {
    if (!endpoint || !token) {
      setError('Please enter endpoint and token');
      return;
    }

    setTesting(true);
    setTestResult(null);
    setError(null);

    try {
      const client = getClient({
        id: 'test',
        name: 'test',
        endpoint,
        token,
        addedAt: Date.now(),
      });

      const connected = await client.testConnection();
      setTestResult(connected ? 'success' : 'error');
      if (!connected) {
        setError('Failed to connect to server');
      }
    } catch (err) {
      setTestResult('error');
      setError('Connection failed: ' + String(err));
    } finally {
      setTesting(false);
    }
  };

  const handleSubmit = async () => {
    if (!endpoint || !token) {
      setError('Please enter endpoint and token');
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      const serverName = name || new URL(endpoint.startsWith('http') ? endpoint : 'http://' + endpoint).hostname;

      if (isEditMode && onUpdate && editServer) {
        await onUpdate(editServer.id, serverName, endpoint, token);
      } else {
        await onAdd(serverName, endpoint, token);
      }
      handleClose();
    } catch (err) {
      setError(`Failed to ${isEditMode ? 'update' : 'add'} server: ` + String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>{isEditMode ? 'Edit Server' : 'Add Server'}</DialogTitle>
      <DialogContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
          {error && (
            <Alert severity="error" onClose={() => setError(null)}>
              {error}
            </Alert>
          )}

          {testResult === 'success' && (
            <Alert severity="success">
              Connection successful!
            </Alert>
          )}

          <TextField
            label="Server Name (optional)"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="My Server"
            fullWidth
          />

          <TextField
            label="Server URL"
            value={endpoint}
            onChange={(e) => setEndpoint(e.target.value)}
            placeholder="http://192.168.1.10:8080/v1"
            helperText="Full API URL including /v1 path (e.g., http://localhost:8080/v1)"
            required
            fullWidth
          />

          <TextField
            label="JWT Token"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="eyJhbGciOiJIUzI1NiIs..."
            required
            fullWidth
            multiline
            rows={3}
          />

          <Button
            variant="outlined"
            onClick={handleTestConnection}
            disabled={testing || !endpoint || !token}
          >
            {testing ? <CircularProgress size={20} sx={{ mr: 1 }} /> : null}
            Test Connection
          </Button>
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose}>Cancel</Button>
        <Button
          variant="contained"
          onClick={handleSubmit}
          disabled={submitting || !endpoint || !token}
        >
          {submitting ? <CircularProgress size={20} sx={{ mr: 1 }} /> : null}
          {isEditMode ? 'Save' : 'Add Server'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
