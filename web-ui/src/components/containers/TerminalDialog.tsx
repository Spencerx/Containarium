'use client';

import { useEffect, useRef, useState, useCallback } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  IconButton,
  Box,
  Typography,
  CircularProgress,
} from '@mui/material';
import CloseIcon from '@mui/icons-material/Close';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';

interface TerminalDialogProps {
  open: boolean;
  onClose: () => void;
  containerName: string;
  username: string;
  serverEndpoint: string;
  token: string;
}

interface TerminalMessage {
  type: 'input' | 'output' | 'resize' | 'error';
  data?: string;
  cols?: number;
  rows?: number;
}

export default function TerminalDialog({
  open,
  onClose,
  containerName,
  username,
  serverEndpoint,
  token,
}: TerminalDialogProps) {
  console.log('TerminalDialog render:', { open, containerName, username, serverEndpoint: serverEndpoint?.substring(0, 50) });

  const terminalRef = useRef<HTMLDivElement>(null);
  const terminalInstance = useRef<Terminal | null>(null);
  const fitAddon = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);

  // Cleanup function
  const cleanup = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    if (terminalInstance.current) {
      terminalInstance.current.dispose();
      terminalInstance.current = null;
    }
    fitAddon.current = null;
    setConnected(false);
    setConnecting(false);
    setError(null);
  }, []);

  // Connect to terminal WebSocket
  const connect = useCallback(() => {
    console.log('connect() called:', { terminalRef: !!terminalRef.current, connecting, connected });
    if (!terminalRef.current || connecting || connected) return;

    setConnecting(true);
    setError(null);

    // Create terminal
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#d4d4d4',
        selectionBackground: '#264f78',
      },
    });

    const fit = new FitAddon();
    const webLinks = new WebLinksAddon();

    term.loadAddon(fit);
    term.loadAddon(webLinks);
    term.open(terminalRef.current);
    fit.fit();

    terminalInstance.current = term;
    fitAddon.current = fit;

    // Build WebSocket URL
    let wsUrl = serverEndpoint.replace(/^http/, 'ws');
    // Remove /v1 suffix if present for the terminal endpoint
    wsUrl = wsUrl.replace(/\/v1\/?$/, '');
    // Sanitize token - remove any whitespace/newlines
    const cleanToken = token.replace(/\s+/g, '');
    wsUrl = `${wsUrl}/v1/containers/${username}/terminal?token=${encodeURIComponent(cleanToken)}`;

    console.log('Terminal WebSocket URL:', wsUrl);
    console.log('Server endpoint:', serverEndpoint);
    console.log('Username:', username);

    term.writeln(`Connecting to ${containerName}...`);
    term.writeln(`URL: ${wsUrl.split('?')[0]}`);

    // Connect WebSocket
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setConnected(true);
      setConnecting(false);
      term.writeln('Connected!\r\n');

      // Send initial resize
      const msg: TerminalMessage = {
        type: 'resize',
        cols: term.cols,
        rows: term.rows,
      };
      ws.send(JSON.stringify(msg));
    };

    ws.onmessage = (event) => {
      try {
        const msg: TerminalMessage = JSON.parse(event.data);
        if (msg.type === 'output' && msg.data) {
          term.write(msg.data);
        } else if (msg.type === 'error' && msg.data) {
          term.writeln(`\r\n\x1b[31mError: ${msg.data}\x1b[0m`);
          setError(msg.data);
        }
      } catch {
        // Raw data, write directly
        term.write(event.data);
      }
    };

    ws.onerror = () => {
      setError('WebSocket connection error');
      setConnecting(false);
      term.writeln('\r\n\x1b[31mConnection error\x1b[0m');
    };

    ws.onclose = () => {
      setConnected(false);
      setConnecting(false);
      term.writeln('\r\n\x1b[33mConnection closed\x1b[0m');
    };

    // Handle terminal input
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        const msg: TerminalMessage = {
          type: 'input',
          data: data,
        };
        ws.send(JSON.stringify(msg));
      }
    });

    // Handle resize
    const handleResize = () => {
      if (fitAddon.current && terminalInstance.current) {
        fitAddon.current.fit();
        if (ws.readyState === WebSocket.OPEN) {
          const msg: TerminalMessage = {
            type: 'resize',
            cols: terminalInstance.current.cols,
            rows: terminalInstance.current.rows,
          };
          ws.send(JSON.stringify(msg));
        }
      }
    };

    window.addEventListener('resize', handleResize);

    return () => {
      window.removeEventListener('resize', handleResize);
    };
  }, [serverEndpoint, username, token, containerName, connecting, connected]);

  // Connect when dialog opens
  useEffect(() => {
    console.log('useEffect triggered:', { open, hasRef: !!terminalRef.current });
    if (open) {
      // Delay to ensure DOM is ready and ref is attached
      const timer = setTimeout(() => {
        console.log('setTimeout callback:', { hasRef: !!terminalRef.current });
        if (terminalRef.current) {
          connect();
        } else {
          console.log('terminalRef still null, retrying...');
          // Retry after another delay
          setTimeout(() => {
            if (terminalRef.current) {
              connect();
            }
          }, 200);
        }
      }, 100);
      return () => clearTimeout(timer);
    }
  }, [open, connect]);

  // Cleanup when dialog closes
  useEffect(() => {
    if (!open) {
      cleanup();
    }
  }, [open, cleanup]);

  // Fit terminal when dialog resizes
  useEffect(() => {
    if (open && fitAddon.current) {
      const timer = setTimeout(() => {
        fitAddon.current?.fit();
      }, 100);
      return () => clearTimeout(timer);
    }
  }, [open]);

  const handleClose = () => {
    cleanup();
    onClose();
  };

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      maxWidth="lg"
      fullWidth
      PaperProps={{
        sx: {
          height: '80vh',
          maxHeight: '80vh',
        },
      }}
    >
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', py: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography variant="h6">
            Terminal: {containerName}
          </Typography>
          {connecting && <CircularProgress size={20} />}
          {connected && (
            <Typography variant="caption" sx={{ color: 'success.main' }}>
              Connected
            </Typography>
          )}
          {error && (
            <Typography variant="caption" sx={{ color: 'error.main' }}>
              {error}
            </Typography>
          )}
        </Box>
        <IconButton onClick={handleClose} size="small">
          <CloseIcon />
        </IconButton>
      </DialogTitle>
      <DialogContent sx={{ p: 0, bgcolor: '#1e1e1e', overflow: 'hidden' }}>
        <Box
          ref={terminalRef}
          sx={{
            width: '100%',
            height: '100%',
            '& .xterm': {
              height: '100%',
              padding: '8px',
            },
          }}
        />
      </DialogContent>
    </Dialog>
  );
}
