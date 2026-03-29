import React from 'react';
import {colors} from '../theme';

interface Hint {
  key: string;
  label: string;
}

interface StatusBarProps {
  connectionInfo: string;
  hints: Hint[];
}

export const StatusBar: React.FC<StatusBarProps> = ({connectionInfo, hints}) => {
  return (
    <div style={{
      marginTop: 'auto',
      background: colors.bgPanel,
      borderRadius: 4,
      padding: '4px 12px',
      fontSize: 14,
    }}>
      <div style={{marginBottom: 4}}>
        <span style={{color: colors.cyan}}>{connectionInfo}</span>
      </div>
      <div style={{display: 'flex', gap: 8, flexWrap: 'wrap'}}>
        {hints.map((h) => (
          <span key={h.key}>
            <span style={{
              background: colors.cyan,
              color: colors.bgPanel,
              padding: '1px 5px',
              borderRadius: 2,
              fontSize: 12,
              fontWeight: 'bold',
            }}>{h.key}</span>
            <span style={{color: colors.white, marginLeft: 3, fontSize: 13}}>{h.label}</span>
          </span>
        ))}
      </div>
    </div>
  );
};
