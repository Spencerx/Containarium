'use client';

import {
  Card,
  CardContent,
  Typography,
  Box,
  Chip,
  IconButton,
  Tooltip,
  Link,
} from '@mui/material';
import DeleteIcon from '@mui/icons-material/Delete';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import RouteIcon from '@mui/icons-material/AltRoute';
import { ProxyRoute } from '@/src/types/app';

interface RouteCardProps {
  route: ProxyRoute;
  onDelete?: (domain: string) => void;
}

export default function RouteCard({ route, onDelete }: RouteCardProps) {
  const fullUrl = `https://${route.fullDomain}`;

  return (
    <Card
      sx={{
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        border: '1px solid',
        borderColor: 'divider',
      }}
    >
      <CardContent sx={{ flex: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 2 }}>
          <RouteIcon sx={{ mr: 1, color: 'primary.main' }} />
          <Typography variant="h6" component="div" sx={{ flex: 1 }}>
            {route.fullDomain}
          </Typography>
          <Chip
            label={route.active ? 'Active' : 'Inactive'}
            color={route.active ? 'success' : 'default'}
            size="small"
          />
        </Box>

        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
          <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
            <Typography variant="body2" color="text.secondary">
              Target:
            </Typography>
            <Typography variant="body2" fontFamily="monospace">
              {route.containerIp || 'N/A'}:{route.port || 'N/A'}
            </Typography>
          </Box>

          {route.appName && (
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                App:
              </Typography>
              <Typography variant="body2">
                {route.appName}
              </Typography>
            </Box>
          )}

          {route.username && (
            <Box sx={{ display: 'flex', justifyContent: 'space-between' }}>
              <Typography variant="body2" color="text.secondary">
                Owner:
              </Typography>
              <Typography variant="body2">
                {route.username}
              </Typography>
            </Box>
          )}
        </Box>

        <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 2, gap: 1 }}>
          <Tooltip title="Open in new tab">
            <IconButton
              size="small"
              component={Link}
              href={fullUrl}
              target="_blank"
              rel="noopener noreferrer"
            >
              <OpenInNewIcon fontSize="small" />
            </IconButton>
          </Tooltip>
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
        </Box>
      </CardContent>
    </Card>
  );
}
