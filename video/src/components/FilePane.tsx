import React from 'react';
import {colors} from '../theme';

export interface FileEntry {
  name: string;
  size: string;
  type: 'dir' | 'file' | 'exec' | 'symlink';
}

interface FilePaneProps {
  title: string;
  files: FileEntry[];
  cursorIndex: number;
  selectedIndices: number[];
  active: boolean;
}

const fileColor = (type: FileEntry['type']): string => {
  switch (type) {
    case 'dir': return colors.cyan;
    case 'exec': return colors.green;
    case 'symlink': return colors.cyan;
    default: return colors.white;
  }
};

const fileSuffix = (type: FileEntry['type']): string => {
  switch (type) {
    case 'dir': return '/';
    case 'exec': return '*';
    case 'symlink': return '@';
    default: return '';
  }
};

export const FilePane: React.FC<FilePaneProps> = ({
  title, files, cursorIndex, selectedIndices, active,
}) => {
  const borderColor = active ? colors.cyan : colors.dim;

  return (
    <div style={{
      flex: 1,
      border: `1px solid ${borderColor}`,
      borderRadius: 8,
      padding: 10,
      display: 'flex',
      flexDirection: 'column',
      overflow: 'hidden',
    }}>
      <div style={{
        color: colors.cyan,
        fontWeight: 'bold',
        marginBottom: 8,
        fontSize: 15,
      }}>
        {title}
      </div>
      <div style={{flex: 1}}>
        {files.map((f, i) => {
          const isCursor = i === cursorIndex;
          const isSelected = selectedIndices.includes(i);
          let bg = 'transparent';
          if (isSelected) bg = colors.teal;
          else if (isCursor) bg = colors.cursorBg;

          return (
            <div key={f.name} style={{
              display: 'flex',
              justifyContent: 'space-between',
              padding: '2px 6px',
              borderRadius: 3,
              background: bg,
            }}>
              <span style={{
                color: fileColor(f.type),
                fontWeight: f.type === 'dir' ? 'bold' : 'normal',
                fontStyle: f.type === 'symlink' ? 'italic' : 'normal',
              }}>
                {f.name}{fileSuffix(f.type)}
              </span>
              <span style={{color: colors.dim}}>{f.size}</span>
            </div>
          );
        })}
      </div>
      <div style={{color: colors.dim, fontSize: 12, marginTop: 8}}>
        {files.length} items
        {selectedIndices.length > 0 && ` | ${selectedIndices.length} selected`}
      </div>
    </div>
  );
};
