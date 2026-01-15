'use client';

import { AppBar as MuiAppBar, Toolbar, Typography, Button, Box } from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import StorageIcon from '@mui/icons-material/Storage';

interface AppBarProps {
  onAddServer: () => void;
}

export default function AppBar({ onAddServer }: AppBarProps) {
  return (
    <MuiAppBar position="static" elevation={1}>
      <Toolbar>
        <StorageIcon sx={{ mr: 2 }} />
        <Typography variant="h6" component="div" sx={{ flexGrow: 1 }}>
          Containarium
        </Typography>
        <Button
          color="inherit"
          startIcon={<AddIcon />}
          onClick={onAddServer}
        >
          Add Server
        </Button>
      </Toolbar>
    </MuiAppBar>
  );
}
