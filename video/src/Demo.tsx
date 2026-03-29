import React from 'react';
import {AbsoluteFill} from 'remotion';
import {colors} from './theme';

export const Demo: React.FC = () => {
  return (
    <AbsoluteFill style={{backgroundColor: colors.bgDark, color: colors.white}}>
      <div>Placeholder</div>
    </AbsoluteFill>
  );
};
