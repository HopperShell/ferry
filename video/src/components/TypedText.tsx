import React from 'react';
import {Cursor} from './Cursor';

interface TypedTextProps {
  text: string;
  progress: number; // 0–1
  showCursor?: boolean;
}

export const TypedText: React.FC<TypedTextProps> = ({text, progress, showCursor = true}) => {
  const charCount = Math.floor(progress * text.length);
  const displayed = text.slice(0, charCount);

  return (
    <span>
      {displayed}
      {showCursor && <Cursor visible={progress < 1} />}
    </span>
  );
};
