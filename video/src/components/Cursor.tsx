import React from 'react';
import {useCurrentFrame} from 'remotion';
import {colors} from '../theme';

export const Cursor: React.FC<{visible?: boolean}> = ({visible = true}) => {
  const frame = useCurrentFrame();
  const blink = Math.floor(frame / 15) % 2 === 0;

  if (!visible) return null;

  return (
    <span style={{
      color: colors.cyan,
      opacity: blink ? 1 : 0,
    }}>
      ▌
    </span>
  );
};
