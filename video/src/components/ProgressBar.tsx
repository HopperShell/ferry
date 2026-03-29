import React from 'react';
import {colors} from '../theme';

interface ProgressBarProps {
  progress: number; // 0–1
  width: number;    // character count
}

export const ProgressBar: React.FC<ProgressBarProps> = ({progress, width}) => {
  const filled = Math.round(progress * width);
  const empty = width - filled;

  return (
    <span>
      <span style={{color: colors.amber}}>{'█'.repeat(filled)}</span>
      <span style={{color: colors.dim}}>{'░'.repeat(empty)}</span>
      <span style={{color: colors.dim}}> {Math.round(progress * 100)}%</span>
    </span>
  );
};
