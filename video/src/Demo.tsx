import React from 'react';
import {useCurrentFrame, interpolate, Sequence, AbsoluteFill} from 'remotion';
import {Terminal} from './components/Terminal';
import {ConnectionPicker} from './scenes/ConnectionPicker';
import {FileBrowser} from './scenes/FileBrowser';
import {SyncView} from './scenes/SyncView';
import {colors} from './theme';

const FADE_DURATION = 6;

const FadeIn: React.FC<{children: React.ReactNode; durationInFrames: number}> = ({
  children, durationInFrames,
}) => {
  const frame = useCurrentFrame();

  const fadeIn = interpolate(frame, [0, FADE_DURATION], [0, 1], {
    extrapolateLeft: 'clamp',
    extrapolateRight: 'clamp',
  });

  const fadeOut = interpolate(
    frame,
    [durationInFrames - FADE_DURATION, durationInFrames],
    [1, 0],
    {extrapolateLeft: 'clamp', extrapolateRight: 'clamp'},
  );

  return (
    <AbsoluteFill style={{opacity: Math.min(fadeIn, fadeOut)}}>
      {children}
    </AbsoluteFill>
  );
};

export const Demo: React.FC = () => {
  return (
    <AbsoluteFill style={{backgroundColor: colors.bgDark}}>
      <Sequence durationInFrames={210}>
        <FadeIn durationInFrames={210}>
          <Terminal>
            <ConnectionPicker />
          </Terminal>
        </FadeIn>
      </Sequence>

      <Sequence from={210} durationInFrames={270}>
        <FadeIn durationInFrames={270}>
          <Terminal>
            <FileBrowser />
          </Terminal>
        </FadeIn>
      </Sequence>

      <Sequence from={480} durationInFrames={270}>
        <FadeIn durationInFrames={270}>
          <Terminal>
            <SyncView />
          </Terminal>
        </FadeIn>
      </Sequence>
    </AbsoluteFill>
  );
};
