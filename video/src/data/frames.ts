import {FileEntry} from '../components/FilePane';

// ── Types ──

export interface PickerFrame {
  frame: number;
  searchText: string;
  hosts: {name: string; detail: string}[];
  cursorIndex: number;
  connecting?: boolean;
}

export interface BrowserFrame {
  frame: number;
  activePane: 'left' | 'right';
  leftFiles: FileEntry[];
  rightFiles: FileEntry[];
  leftCursor: number;
  rightCursor: number;
  selectedLeft: number[];
  selectedRight: number[];
  leftPath: string;
  rightPath: string;
}

export interface SyncEntry {
  status: 'local-only' | 'remote-only' | 'local-newer' | 'remote-newer';
  name: string;
  size: string;
}

export interface SyncFrame {
  frame: number;
  entries: SyncEntry[];
  cursorIndex: number;
  selectedIndices: number[];
  progress?: number;
  doneMessage?: string;
}

// ── Shared data ──

const allHosts = [
  {name: 'dev-api-01', detail: 'dev@10.0.0.5:22'},
  {name: 'staging-web', detail: 'deploy@10.0.0.20:22'},
  {name: 'prod-web-01', detail: 'deploy@10.0.1.10:22'},
  {name: 'prod-web-02', detail: 'deploy@10.0.1.11:22'},
  {name: 'prod-db-01', detail: 'admin@10.0.2.5:22'},
];

const prodHosts = allHosts.filter(h => h.name.startsWith('prod'));

const localFiles: FileEntry[] = [
  {name: 'src', size: '', type: 'dir'},
  {name: 'config', size: '', type: 'dir'},
  {name: 'README.md', size: '2.1 KB', type: 'file'},
  {name: 'package.json', size: '1.4 KB', type: 'file'},
  {name: 'deploy.sh', size: '892 B', type: 'exec'},
  {name: '.env.example', size: '256 B', type: 'file'},
];

const remoteFiles: FileEntry[] = [
  {name: 'src', size: '', type: 'dir'},
  {name: 'config', size: '', type: 'dir'},
  {name: 'README.md', size: '2.1 KB', type: 'file'},
  {name: 'package.json', size: '1.2 KB', type: 'file'},
  {name: 'deploy.sh', size: '890 B', type: 'exec'},
  {name: '.env', size: '512 B', type: 'file'},
];

const remoteSrcFiles: FileEntry[] = [
  {name: '..', size: '', type: 'dir'},
  {name: 'index.ts', size: '3.2 KB', type: 'file'},
  {name: 'app.ts', size: '8.1 KB', type: 'file'},
  {name: 'config.ts', size: '1.5 KB', type: 'file'},
  {name: 'utils.ts', size: '2.0 KB', type: 'file'},
  {name: 'types.ts', size: '960 B', type: 'file'},
];

const syncEntries: SyncEntry[] = [
  {status: 'local-only', name: 'index.ts', size: '3.2 KB'},
  {status: 'local-newer', name: 'app.ts', size: '8.1 KB'},
  {status: 'remote-only', name: 'config.ts', size: '1.5 KB'},
];

// ── Scene 1: Connection Picker (frames 0–210) ──

export const pickerFrames: PickerFrame[] = [
  {frame: 0, searchText: '', hosts: allHosts, cursorIndex: 0},
  {frame: 45, searchText: '', hosts: allHosts, cursorIndex: 0},
  {frame: 55, searchText: 'p', hosts: allHosts.filter(h => h.name.includes('p')), cursorIndex: 0},
  {frame: 62, searchText: 'pr', hosts: prodHosts, cursorIndex: 0},
  {frame: 69, searchText: 'pro', hosts: prodHosts, cursorIndex: 0},
  {frame: 76, searchText: 'prod', hosts: prodHosts, cursorIndex: 0},
  {frame: 120, searchText: 'prod', hosts: prodHosts, cursorIndex: 0},
  {frame: 130, searchText: 'prod', hosts: prodHosts, cursorIndex: 0, connecting: true},
  {frame: 190, searchText: 'prod', hosts: prodHosts, cursorIndex: 0, connecting: true},
];

// ── Scene 2: File Browser (frames 0–270, maps to global 210–480) ──

export const browserFrames: BrowserFrame[] = [
  {frame: 0, activePane: 'left', leftFiles: localFiles, rightFiles: remoteFiles,
   leftCursor: 0, rightCursor: 0, selectedLeft: [], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp'},
  {frame: 20, activePane: 'left', leftFiles: localFiles, rightFiles: remoteFiles,
   leftCursor: 0, rightCursor: 0, selectedLeft: [], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp'},
  {frame: 35, activePane: 'left', leftFiles: localFiles, rightFiles: remoteFiles,
   leftCursor: 1, rightCursor: 0, selectedLeft: [], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp'},
  {frame: 50, activePane: 'left', leftFiles: localFiles, rightFiles: remoteFiles,
   leftCursor: 2, rightCursor: 0, selectedLeft: [], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp'},
  {frame: 65, activePane: 'left', leftFiles: localFiles, rightFiles: remoteFiles,
   leftCursor: 3, rightCursor: 0, selectedLeft: [], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp'},
  {frame: 90, activePane: 'right', leftFiles: localFiles, rightFiles: remoteFiles,
   leftCursor: 3, rightCursor: 0, selectedLeft: [], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp'},
  {frame: 115, activePane: 'right', leftFiles: localFiles, rightFiles: remoteSrcFiles,
   leftCursor: 3, rightCursor: 0, selectedLeft: [], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp/src'},
  {frame: 140, activePane: 'left', leftFiles: localFiles, rightFiles: remoteSrcFiles,
   leftCursor: 3, rightCursor: 0, selectedLeft: [], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp/src'},
  {frame: 165, activePane: 'left', leftFiles: localFiles, rightFiles: remoteSrcFiles,
   leftCursor: 3, rightCursor: 0, selectedLeft: [3], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp/src'},
  {frame: 180, activePane: 'left', leftFiles: localFiles, rightFiles: remoteSrcFiles,
   leftCursor: 4, rightCursor: 0, selectedLeft: [3], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp/src'},
  {frame: 200, activePane: 'left', leftFiles: localFiles, rightFiles: remoteSrcFiles,
   leftCursor: 4, rightCursor: 0, selectedLeft: [3, 4], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp/src'},
  {frame: 250, activePane: 'left', leftFiles: localFiles, rightFiles: remoteSrcFiles,
   leftCursor: 4, rightCursor: 0, selectedLeft: [3, 4], selectedRight: [],
   leftPath: '~/projects/myapp', rightPath: '/var/www/myapp/src'},
];

// ── Scene 3: Sync/Diff (frames 0–270, maps to global 480–750) ──

export const syncFrames: SyncFrame[] = [
  {frame: 0, entries: syncEntries, cursorIndex: 0, selectedIndices: []},
  {frame: 45, entries: syncEntries, cursorIndex: 0, selectedIndices: []},
  {frame: 60, entries: syncEntries, cursorIndex: 0, selectedIndices: [0, 1, 2]},
  {frame: 90, entries: syncEntries, cursorIndex: 0, selectedIndices: [0, 1, 2]},
  {frame: 105, entries: syncEntries, cursorIndex: 0, selectedIndices: [0, 1, 2], progress: 0},
  {frame: 120, entries: syncEntries, cursorIndex: 0, selectedIndices: [0, 1, 2], progress: 0.33},
  {frame: 135, entries: syncEntries, cursorIndex: 0, selectedIndices: [0, 1, 2], progress: 0.66},
  {frame: 150, entries: syncEntries, cursorIndex: 0, selectedIndices: [0, 1, 2], progress: 1.0},
  {frame: 160, entries: syncEntries, cursorIndex: 0, selectedIndices: [0, 1, 2],
   progress: 1.0, doneMessage: '3/3 files synced'},
  {frame: 240, entries: syncEntries, cursorIndex: 0, selectedIndices: [0, 1, 2],
   progress: 1.0, doneMessage: '3/3 files synced'},
];

// ── Helper ──

export function getFrameState<T extends {frame: number}>(frames: T[], currentFrame: number): T {
  let state = frames[0];
  for (const f of frames) {
    if (f.frame <= currentFrame) {
      state = f;
    } else {
      break;
    }
  }
  return state;
}
