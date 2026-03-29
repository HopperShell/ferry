import React from 'react';
import {useCurrentFrame} from 'remotion';
import {FilePane} from '../components/FilePane';
import {StatusBar} from '../components/StatusBar';
import {browserFrames, getFrameState} from '../data/frames';

const hints = [
  {key: 'Tab', label: 'Switch'},
  {key: 'Space', label: 'Select'},
  {key: 'yy', label: 'Copy'},
  {key: 'p', label: 'Paste'},
  {key: 'dd', label: 'Delete'},
  {key: 'S', label: 'Sync'},
  {key: '?', label: 'Help'},
];

export const FileBrowser: React.FC = () => {
  const frame = useCurrentFrame();
  const state = getFrameState(browserFrames, frame);

  return (
    <div style={{display: 'flex', flexDirection: 'column', flex: 1}}>
      <div style={{display: 'flex', gap: 8, flex: 1}}>
        <FilePane
          title={`Local: ${state.leftPath}`}
          files={state.leftFiles}
          cursorIndex={state.activePane === 'left' ? state.leftCursor : -1}
          selectedIndices={state.selectedLeft}
          active={state.activePane === 'left'}
        />
        <FilePane
          title={`Remote: ${state.rightPath}`}
          files={state.rightFiles}
          cursorIndex={state.activePane === 'right' ? state.rightCursor : -1}
          selectedIndices={state.selectedRight}
          active={state.activePane === 'right'}
        />
      </div>
      <StatusBar
        connectionInfo="deploy@prod-web-01:22"
        hints={hints}
      />
    </div>
  );
};
