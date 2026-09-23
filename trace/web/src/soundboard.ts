// Soundboard integration — plays distinctive sounds for crash events via Web Audio API.
// No external audio files required; all sounds are synthesized in real-time.

export interface SoundProfile {
  frequency: number
  waveform: 'sine' | 'square' | 'sawtooth' | 'triangle'
  duration_ms: number
  volume: number
  pitch_bend: number
  repeats: number
  gap_ms: number
  description: string
}

export interface CrashEvent {
  id: string
  event_type: string
  severity: 'ok' | 'warn' | 'error' | 'fatal'
  architecture: string
  device_id: string
  fingerprint: string
  timestamp: string
}

// SoundManager handles Web Audio API context and playback.
export class SoundManager {
  private ctx: AudioContext | null = null
  private enabled = false
  private volume = 0.5

  // Profile for each crash type
  private profiles: Record<string, SoundProfile> = {
    hardfault: {
      frequency: 120, waveform: 'sawtooth', duration_ms: 600,
      volume: 0.8, pitch_bend: -40, repeats: 2, gap_ms: 300,
      description: 'Grave alert buzz',
    },
    memmanage: {
      frequency: 1800, waveform: 'square', duration_ms: 150,
      volume: 0.6, pitch_bend: 0, repeats: 1, gap_ms: 0,
      description: 'Sharp high beep',
    },
    busfault: {
      frequency: 250, waveform: 'sawtooth', duration_ms: 400,
      volume: 0.7, pitch_bend: 20, repeats: 3, gap_ms: 120,
      description: 'Static crunch',
    },
    watchdog: {
      frequency: 880, waveform: 'square', duration_ms: 200,
      volume: 0.9, pitch_bend: 0, repeats: 4, gap_ms: 250,
      description: 'Digital alarm',
    },
    brownout: {
      frequency: 90, waveform: 'sine', duration_ms: 1200,
      volume: 0.6, pitch_bend: -80, repeats: 1, gap_ms: 0,
      description: 'Power drop sound',
    },
    ota_failure: {
      frequency: 330, waveform: 'triangle', duration_ms: 500,
      volume: 0.7, pitch_bend: -60, repeats: 2, gap_ms: 400,
      description: 'OTA error tone',
    },
    network_loss: {
      frequency: 440, waveform: 'sine', duration_ms: 800,
      volume: 0.5, pitch_bend: -100, repeats: 1, gap_ms: 0,
      description: 'Disconnect hum',
    },
    sensor_timeout: {
      frequency: 660, waveform: 'sine', duration_ms: 1500,
      volume: 0.4, pitch_bend: 0, repeats: 1, gap_ms: 0,
      description: 'Long sensor beep',
    },
    stack_overflow: {
      frequency: 160, waveform: 'sawtooth', duration_ms: 350,
      volume: 0.75, pitch_bend: -30, repeats: 3, gap_ms: 180,
      description: 'Gritty stack overflow',
    },
    secure_fault: {
      frequency: 110, waveform: 'square', duration_ms: 900,
      volume: 0.85, pitch_bend: -50, repeats: 2, gap_ms: 500,
      description: 'Deep secure alarm',
    },
    data_fault: {
      frequency: 2000, waveform: 'square', duration_ms: 100,
      volume: 0.5, pitch_bend: 0, repeats: 2, gap_ms: 80,
      description: 'Quick data fault click',
    },
  }

  setEnabled(enabled: boolean): void {
    this.enabled = enabled
    if (enabled && !this.ctx) {
      this.initContext()
    }
  }

  setVolume(vol: number): void {
    this.volume = Math.max(0, Math.min(1, vol))
  }

  addProfile(key: string, profile: SoundProfile): void {
    this.profiles[key] = profile
  }

  private initContext(): void {
    try {
      const Ctx = (window as any).AudioContext || (window as any).webkitAudioContext
      if (Ctx) {
        this.ctx = new Ctx()
      }
    } catch { /* Audio not available */ }
  }

  playForEvent(event: CrashEvent): void {
    if (!this.enabled || !this.ctx) return
    if (event.severity !== 'error' && event.severity !== 'fatal') return

    const profile = this.profiles[event.event_type] || this.profiles['hardfault']
    this.playSound(profile)
  }

  private playSound(profile: SoundProfile): void {
    if (!this.ctx) return
    if (this.ctx.state === 'suspended') {
      this.ctx.resume()
    }

    const totalDuration = profile.duration_ms + profile.gap_ms * (profile.repeats - 1)
    const sampleRate = this.ctx.sampleRate
    const totalSamples = Math.floor(sampleRate * totalDuration / 1000)
    const buffer = this.ctx.createBuffer(1, totalSamples, sampleRate)
    const data = buffer.getChannelData(0)

    for (let repeat = 0; repeat < profile.repeats; repeat++) {
      const repeatStart = repeat * (profile.duration_ms + profile.gap_ms) * sampleRate / 1000
      const samplesInRepeat = Math.floor(profile.duration_ms * sampleRate / 1000)

      for (let i = 0; i < samplesInRepeat; i++) {
        const t = i / sampleRate
        const freq = profile.frequency + profile.pitch_bend * (t / (profile.duration_ms / 1000))
        const clampedFreq = Math.max(20, freq)

        const phase = 2 * Math.PI * clampedFreq * t
        let wave: number
        switch (profile.waveform) {
          case 'square':
            wave = Math.sin(phase) >= 0 ? 1 : -1
            break
          case 'sawtooth': {
            const frac = (clampedFreq * t) % 1
            wave = 2 * frac - 1
            break
          }
          case 'triangle': {
            const frac = (clampedFreq * t) % 1
            wave = frac < 0.5 ? 4 * frac - 1 : 3 - 4 * frac
            break
          }
          default:
            wave = Math.sin(phase)
        }

        // Amplitude envelope (attack-decay)
        const attackSamples = sampleRate / 50
        const decaySamples = sampleRate / 10
        let amp = profile.volume
        if (i < attackSamples) {
          amp = profile.volume * (i / attackSamples)
        } else if (i < attackSamples + decaySamples) {
          const f = (i - attackSamples) / decaySamples
          amp = profile.volume * (1 - 0.3 * f)
        }

        const idx = Math.floor(repeatStart + i)
        if (idx < totalSamples) {
          data[idx] = wave * amp
        }
      }
    }

    const source = this.ctx.createBufferSource()
    source.buffer = buffer
    const gain = this.ctx.createGain()
    gain.gain.value = this.volume
    source.connect(gain)
    gain.connect(this.ctx.destination)
    source.start()
  }
}

// Singleton sound manager instance
export const soundManager = new SoundManager()

// Check if Web Audio is supported
export function isAudioSupported(): boolean {
  return !!(window as any).AudioContext || !!(window as any).webkitAudioContext
}
