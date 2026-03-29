import React from 'react';
import {useCurrentFrame} from 'remotion';
import {colors} from '../theme';
import {ProgressBar} from '../components/ProgressBar';
import {syncFrames, getFrameState, SyncEntry} from '../data/frames';

const statusLabel = (status: SyncEntry['status']): {text: string; color: string} => {
  switch (status) {
    case 'local-only': return {text: 'local → ', color: colors.cyan};
    case 'remote-only': return {text: '← remote', color: colors.amber};
    case 'local-newer': return {text: 'local ≠ ', color: colors.cyan};
    case 'remote-newer': return {text: ' ≠ remote', color: colors.amber};
  }
};

export const SyncView: React.FC = () => {
  const frame = useCurrentFrame();
  const state = getFrameState(syncFrames, frame);

  return (
    <div style={{
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      flex: 1,
    }}>
      <div style={{
        border: `1px solid ${colors.cyan}`,
        borderRadius: 8,
        padding: 16,
        width: 700,
      }}>
        <div style={{
          color: colors.cyan,
          fontWeight: 'bold',
          marginBottom: 12,
          fontSize: 15,
        }}>
          Sync src/  {state.entries.length} differ
          {state.selectedIndices.length > 0 && `, ${state.selectedIndices.length} selected`}
        </div>

        <div style={{
          display: 'flex',
          color: colors.dim,
          fontSize: 13,
          marginBottom: 4,
          padding: '0 6px',
        }}>
          <span style={{width: 30}}></span>
          <span style={{width: 90}}>Status</span>
          <span style={{flex: 1}}>Name</span>
          <span style={{width: 80, textAlign: 'right'}}>Size</span>
        </div>

        {state.entries.map((entry, i) => {
          const isCursor = i === state.cursorIndex;
          const isSelected = state.selectedIndices.includes(i);
          const label = statusLabel(entry.status);
          let bg = 'transparent';
          if (isSelected) bg = colors.teal;
          else if (isCursor) bg = colors.cursorBg;

          return (
            <div key={entry.name} style={{
              display: 'flex',
              padding: '3px 6px',
              borderRadius: 3,
              background: bg,
            }}>
              <span style={{width: 30}}>
                {isCursor ? '» ' : '  '}
                {isSelected && !isCursor ? ' *' : ''}
              </span>
              <span style={{width: 90, color: label.color}}>{label.text}</span>
              <span style={{flex: 1}}>{entry.name}</span>
              <span style={{width: 80, textAlign: 'right', color: colors.dim}}>{entry.size}</span>
            </div>
          );
        })}

        <div style={{color: colors.dim, fontSize: 13, marginTop: 12, paddingLeft: 6}}>
          (12 entries in sync, not shown)
        </div>

        {state.progress !== undefined && (
          <div style={{marginTop: 12, paddingLeft: 6}}>
            <ProgressBar progress={state.progress} width={30} />
          </div>
        )}

        {state.doneMessage && (
          <div style={{
            color: colors.green,
            fontWeight: 'bold',
            marginTop: 8,
            paddingLeft: 6,
          }}>
            ✓ {state.doneMessage}
          </div>
        )}

        <div style={{color: colors.dim, fontSize: 13, marginTop: 12, paddingLeft: 6}}>
          j/k:nav  Space:select  a:all  →:push  ←:pull  Esc:back
        </div>
      </div>
    </div>
  );
};
