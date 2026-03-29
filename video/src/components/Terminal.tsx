import React from 'react';
import {AbsoluteFill} from 'remotion';
import {colors, fontFamily} from '../theme';

export const Terminal: React.FC<{children: React.ReactNode}> = ({children}) => {
  return (
    <AbsoluteFill
      style={{
        backgroundColor: colors.bgDark,
        fontFamily,
        fontSize: 16,
        lineHeight: 1.6,
        color: colors.white,
        padding: 48,
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      {children}
    </AbsoluteFill>
  );
};
