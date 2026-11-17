// Calls the person when the automatic run needs them (S5 and S6, devicerun.ts):
// three short beeps and a vibration of the phone. The shell's vibration
// (spikeNative.vibrate) is used where there is one, because an Android
// WebView does not vibrate for navigator.vibrate; a browser uses the
// Vibration API, where it has one. Neither stops the run when it fails.
import type { SpikeNative } from './native';

/** The part of an AudioContext that the beeps use. */
export interface AudioOut {
  readonly state: AudioContextState;
  readonly currentTime: number;
  readonly destination: AudioNode;
  resume(): Promise<void>;
  createOscillator(): OscillatorNode;
  createGain(): GainNode;
}

export interface AttentionDeps {
  /** A new AudioContext; undefined or a throw where there is none. */
  audio(): AudioOut | undefined;
  /** navigator.vibrate, where the browser has it. */
  vibrate?: (pattern: number[]) => boolean;
  native?: SpikeNative;
}

export interface Attention {
  /**
   * Prepares the sound. Called from the tap on «Rulează tot pentru acest
   * dispozitiv»: browsers and WebViews start sound only after a tap, and the
   * sound comes later, with nobody tapping.
   */
  prime(): void;
  /** The beeps and the vibration. */
  alert(): void;
}

const BEEPS = 3;
const BEEP_EVERY_S = 0.3;
const BEEP_S = 0.15;
const BEEP_HZ = 880;
const VIBRATION_MS = [400, 200, 400];

export function createAttention(deps: AttentionDeps): Attention {
  let audio: AudioOut | undefined;
  return {
    prime() {
      try {
        audio ??= deps.audio();
        if (audio?.state === 'suspended') void audio.resume().catch(() => undefined);
      } catch {
        audio = undefined; // no sound: the vibration is left
      }
    },
    alert() {
      try {
        if (audio) {
          if (audio.state === 'suspended') void audio.resume().catch(() => undefined);
          for (let i = 0; i < BEEPS; i += 1) {
            const at = audio.currentTime + i * BEEP_EVERY_S;
            const oscillator = audio.createOscillator();
            const gain = audio.createGain();
            oscillator.frequency.value = BEEP_HZ;
            gain.gain.value = 0.3;
            oscillator.connect(gain).connect(audio.destination);
            oscillator.start(at);
            oscillator.stop(at + BEEP_S);
          }
        }
      } catch {
        // The sound failed; the vibration may still call the person.
      }
      if (deps.native?.vibrate) {
        deps.native.vibrate().catch(() => undefined);
        return;
      }
      try {
        deps.vibrate?.(VIBRATION_MS);
      } catch {
        // No vibration: the sound is the call.
      }
    },
  };
}
