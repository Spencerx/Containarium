'use client';

import { useState } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Button,
  Alert,
  Box,
  IconButton,
  Typography,
  CircularProgress,
  Divider,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Chip,
  FormControlLabel,
  Checkbox,
} from '@mui/material';
import DeleteIcon from '@mui/icons-material/Delete';
import PersonAddIcon from '@mui/icons-material/PersonAdd';
import { Collaborator, AddCollaboratorRequest } from '@/src/types/container';

interface CollaboratorsDialogProps {
  open: boolean;
  onClose: () => void;
  ownerUsername: string;
  collaborators: Collaborator[];
  isLoading: boolean;
  onAdd: (req: AddCollaboratorRequest) => Promise<{ sshCommand: string }>;
  onRemove: (collaboratorUsername: string) => Promise<void>;
}

export default function CollaboratorsDialog({
  open,
  onClose,
  ownerUsername,
  collaborators,
  isLoading,
  onAdd,
  onRemove,
}: CollaboratorsDialogProps) {
  const [newUsername, setNewUsername] = useState('');
  const [newSSHKey, setNewSSHKey] = useState('');
  const [grantSudo, setGrantSudo] = useState(false);
  const [grantContainerRuntime, setGrantContainerRuntime] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [removingUser, setRemovingUser] = useState<string | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null);

  const handleAdd = async () => {
    const username = newUsername.trim();
    const sshKey = newSSHKey.trim();

    if (!username) {
      setError('Username is required');
      return;
    }
    if (!sshKey) {
      setError('SSH public key is required');
      return;
    }
    if (!sshKey.startsWith('ssh-') && !sshKey.startsWith('ecdsa-') && !sshKey.startsWith('sk-ssh-') && !sshKey.startsWith('sk-ecdsa-')) {
      setError('Invalid SSH public key format');
      return;
    }

    setAdding(true);
    setError(null);
    setSuccess(null);

    try {
      const result = await onAdd({
        collaboratorUsername: username,
        sshPublicKey: sshKey,
        grantSudo,
        grantContainerRuntime,
      });
      setSuccess(`Added ${username}. SSH: ${result.sshCommand}`);
      setNewUsername('');
      setNewSSHKey('');
      setGrantSudo(false);
      setGrantContainerRuntime(false);
    } catch (err) {
      setError(`Failed to add collaborator: ${err instanceof Error ? err.message : err}`);
    } finally {
      setAdding(false);
    }
  };

  const handleRemove = async (collaboratorUsername: string) => {
    setRemovingUser(collaboratorUsername);
    setError(null);
    setSuccess(null);

    try {
      await onRemove(collaboratorUsername);
      setSuccess(`Removed ${collaboratorUsername}`);
      setConfirmRemove(null);
    } catch (err) {
      setError(`Failed to remove: ${err instanceof Error ? err.message : err}`);
    } finally {
      setRemovingUser(null);
    }
  };

  const handleClose = () => {
    if (adding) return;
    setError(null);
    setSuccess(null);
    setNewUsername('');
    setNewSSHKey('');
    setGrantSudo(false);
    setGrantContainerRuntime(false);
    setConfirmRemove(null);
    onClose();
  };

  const formatDate = (unixTimestamp: number): string => {
    if (!unixTimestamp) return '-';
    return new Date(unixTimestamp * 1000).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="md" fullWidth>
      <DialogTitle>
        Collaborators
        <Typography variant="body2" color="text.secondary">
          {ownerUsername}-container
        </Typography>
      </DialogTitle>
      <DialogContent>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
          {error && (
            <Alert severity="error" onClose={() => setError(null)}>
              {error}
            </Alert>
          )}
          {success && (
            <Alert severity="success" onClose={() => setSuccess(null)}>
              {success}
            </Alert>
          )}

          {/* Collaborator list */}
          {isLoading ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
              <CircularProgress size={24} />
            </Box>
          ) : collaborators.length === 0 ? (
            <Typography variant="body2" color="text.secondary" sx={{ py: 2, textAlign: 'center' }}>
              No collaborators yet
            </Typography>
          ) : (
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell><strong>Username</strong></TableCell>
                    <TableCell><strong>Account</strong></TableCell>
                    <TableCell><strong>Permissions</strong></TableCell>
                    <TableCell><strong>Added</strong></TableCell>
                    <TableCell><strong>By</strong></TableCell>
                    <TableCell align="right"><strong>Actions</strong></TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {collaborators.map((c) => (
                    <TableRow key={c.id || c.collaboratorUsername}>
                      <TableCell>{c.collaboratorUsername}</TableCell>
                      <TableCell>
                        <Typography variant="body2" sx={{ fontFamily: 'monospace', fontSize: '0.8rem' }}>
                          {c.accountName}
                        </Typography>
                      </TableCell>
                      <TableCell>
                        <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                          {c.hasSudo && <Chip label="sudo" size="small" color="warning" variant="outlined" />}
                          {c.hasContainerRuntime && <Chip label="docker" size="small" color="info" variant="outlined" />}
                          {!c.hasSudo && !c.hasContainerRuntime && <Typography variant="body2" color="text.secondary">su only</Typography>}
                        </Box>
                      </TableCell>
                      <TableCell>{formatDate(c.addedAt)}</TableCell>
                      <TableCell>{c.createdBy || '-'}</TableCell>
                      <TableCell align="right">
                        {confirmRemove === c.collaboratorUsername ? (
                          <Box sx={{ display: 'flex', gap: 0.5, justifyContent: 'flex-end' }}>
                            <Button
                              size="small"
                              color="error"
                              variant="contained"
                              disabled={removingUser === c.collaboratorUsername}
                              onClick={() => handleRemove(c.collaboratorUsername)}
                            >
                              {removingUser === c.collaboratorUsername ? <CircularProgress size={16} /> : 'Confirm'}
                            </Button>
                            <Button
                              size="small"
                              onClick={() => setConfirmRemove(null)}
                              disabled={removingUser === c.collaboratorUsername}
                            >
                              Cancel
                            </Button>
                          </Box>
                        ) : (
                          <IconButton
                            size="small"
                            color="error"
                            onClick={() => setConfirmRemove(c.collaboratorUsername)}
                          >
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          )}

          <Divider />

          {/* Add collaborator form */}
          <Box>
            <Typography variant="subtitle2" gutterBottom>
              Add Collaborator
            </Typography>
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
              <TextField
                label="Username"
                value={newUsername}
                onChange={(e) => setNewUsername(e.target.value)}
                placeholder="e.g., bob"
                size="small"
                disabled={adding}
              />
              <TextField
                label="SSH Public Key"
                value={newSSHKey}
                onChange={(e) => setNewSSHKey(e.target.value)}
                placeholder="ssh-ed25519 AAAA..."
                size="small"
                multiline
                rows={2}
                disabled={adding}
              />
              <Box sx={{ display: 'flex', gap: 2 }}>
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={grantSudo}
                      onChange={(e) => setGrantSudo(e.target.checked)}
                      size="small"
                      disabled={adding}
                    />
                  }
                  label="Grant full sudo"
                />
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={grantContainerRuntime}
                      onChange={(e) => setGrantContainerRuntime(e.target.checked)}
                      size="small"
                      disabled={adding}
                    />
                  }
                  label="Container runtime (docker)"
                />
              </Box>
              <Box>
                <Button
                  variant="contained"
                  startIcon={adding ? <CircularProgress size={16} /> : <PersonAddIcon />}
                  onClick={handleAdd}
                  disabled={adding || !newUsername.trim() || !newSSHKey.trim()}
                >
                  {adding ? 'Adding...' : 'Add'}
                </Button>
              </Box>
            </Box>
          </Box>
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={adding}>
          Close
        </Button>
      </DialogActions>
    </Dialog>
  );
}
