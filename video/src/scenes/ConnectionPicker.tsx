import React from 'react';
import {useCurrentFrame} from 'remotion';
import {colors} from '../theme';
import {Cursor} from '../components/Cursor';
import {pickerFrames, getFrameState} from '../data/frames';

const LOGO = `   ___
  / _/__ ____________  __
 / _/ -_) __/ __/ // /
/_/ \\__/_/ /_/  \\_, /
               /___/`;

export const ConnectionPicker: React.FC = () => {
  const frame = useCurrentFrame();
  const state = getFrameState(pickerFrames, frame);

  if (state.connecting) {
    const dots = '.'.repeat((Math.floor(frame / 10) % 3) + 1);
    return (
      <div style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        flex: 1,
      }}>
        <span style={{color: colors.cyan}}>
          ⠋ Connecting to prod-web-01{dots}
        </span>
      </div>
    );
  }

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      flex: 1,
      paddingTop: 80,
    }}>
      <pre style={{
        color: colors.cyan,
        fontWeight: 'bold',
        fontSize: 20,
        lineHeight: 1.3,
        textAlign: 'center',
        margin: 0,
      }}>
        {LOGO}
      </pre>
      <div style={{color: colors.dim, marginTop: 8, marginBottom: 24}}>
        secure file transfer, terminal style
      </div>

      <div style={{
        border: `1px solid ${colors.dim}`,
        borderRadius: 4,
        padding: '6px 12px',
        width: 500,
        marginBottom: 16,
      }}>
        <span style={{color: colors.dim}}>Search: </span>
        <span style={{color: colors.cyan}}>{state.searchText}</span>
        <Cursor />
      </div>

      <div style={{width: 500}}>
        {state.hosts.map((h, i) => (
          <div key={h.name} style={{
            padding: '4px 12px',
            borderRadius: 3,
            background: i === state.cursorIndex ? colors.cursorBg : 'transparent',
          }}>
            <span>{h.name}</span>
            <span style={{color: colors.dim, marginLeft: 16}}>{h.detail}</span>
          </div>
        ))}
      </div>

      <div style={{color: colors.dim, marginTop: 24, fontSize: 13}}>
        enter:connect  esc:quit
      </div>
    </div>
  );
};
