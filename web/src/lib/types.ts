export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'

export interface Settings {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number
  day_start_hour: number; retention: number; learning_steps: string; relearning_steps: string; max_interval: number
  new_per_day: number; reviews_per_day: number; new_mix: 'mix' | 'first' | 'last'; daily_goal: number
  show_intervals: boolean; show_timer: boolean; autoplay: boolean; speak: boolean; speak_lang: string
  reminder_time: string; reminder_days: string; reminder_min: number
  notify_streak: boolean; streak_hour: number; notify_weekly: boolean; weekly_day: number; weekly_hour: number; notify_milestones: boolean
  notify_url: string; notify_format: string; notify_configured: boolean
  reader_mode: ConnMode; reader_via: ConnVia; reader_detail: string; reader_open_url: string; reader_configured: boolean
  reader_url: string; reader_token: string; reader_public_url: string
  library_mode: ConnMode; library_via: ConnVia; library_detail: string; library_open_url: string; library_configured: boolean
  library_url: string; library_token: string; library_public_url: string
  notes_mode: ConnMode; notes_via: ConnVia; notes_detail: string; notes_open_url: string; notes_configured: boolean
  notes_url: string; notes_token: string; notes_public_url: string
  time_mode: ConnMode; time_via: ConnVia; time_detail: string; time_open_url: string; time_configured: boolean
  time_url: string; time_token: string; time_public_url: string
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string
}

export type Kind = 'basic' | 'reverse' | 'cloze' | 'type'

export interface DeckCounts {
  total: number; new: number; learning: number; young: number; mature: number; suspended: number
  due_new: number; due_learn: number; due_review: number; done_today: number; new_today: number; due_tomorrow: number
}

export interface Deck {
  id: number; name: string; description: string; color: string; new_per_day: number; reviews_per_day: number; retention: number
  new_order: 'added' | 'random'; archived: boolean; sort: number; source: string; external_id: string; created_at: string; updated_at: string
  counts?: DeckCounts
}

export interface Card {
  id: number; note_id: number; deck_id: number; ord: number; state: 0 | 1 | 2 | 3; step: number; stability: number; difficulty: number
  due: string; last_review: string; interval: number; reps: number; lapses: number; position: number; suspended: boolean; buried_until: string; flag: number
  created_at: string; updated_at: string
}

export interface Note {
  id: number; deck_id: number; kind: Kind; front: string; back: string; extra: string; tags: string
  source: string; source_ref: string; source_url: string; external_id: string; created_at: string; updated_at: string
  deck_name?: string; cards?: Card[]
}

export interface Outcome { rating: 1 | 2 | 3 | 4; days: number; wait_s: number; label: string }

export interface StudyCard extends Card {
  kind: Kind; front: string; back: string; extra: string; tags: string; deck: string; source: string; source_ref: string; source_url: string
  answers: Outcome[]; retrievability: number
}

export interface QueueCounts { new: number; learn: number; review: number; done: number }
export interface NextResult { card: StudyCard | null; counts: QueueCounts; come_back?: string; deck?: Deck }

export interface Dashboard {
  due: number; due_new: number; due_learn: number; due_review: number; done_today: number; minutes_today: number; goal: number
  streak: number; longest_streak: number; due_tomorrow: number; cards: number; decks: number; today: string; come_back?: string
}

export interface Home { dashboard: Dashboard; decks: Deck[]; heat: Record<string, number>; heat_from: string; forecast: number[]; sessions_today?: { count: number; seconds: number } }

/** A study session: one run on the study screen, or study time logged in Pocket Time. */
export interface StudySession {
  id: number; deck_id: number | null; deck_name: string; started_at: string; ended_at: string; last_at: string; date: string
  duration_s: number; cards: number; again: number; source: string; external_id: string; timer_entry_id: number; title: string
  kept_by_time: boolean; elapsed_s: number
}

/** The person's running timer in Pocket Time, as Learn shows it. */
export interface TimeRunning { id: number; name: string; started_in: string; started_at: string; app: string; app_ref: string; mine: boolean }
export interface TimeTimer { connected: boolean; running: TimeRunning | null; detail?: string; open_url?: string; session?: StudySession | null }
export interface StudyStart { session: StudySession | null; conflict?: TimeRunning; time: boolean; detail?: string }

export interface DayStat { day: string; reviews: number; new: number; learn: number; review: number; again: number; mature: number; mature_ok: number; duration_ms: number }
export interface HourStat { hour: number; reviews: number; ok: number; total: number }
export interface Review {
  id: number; card_id: number; deck_id: number; rating: number; state: number; elapsed_days: number; last_interval: number; interval: number
  stability: number; difficulty: number; retrievability: number; duration_ms: number; reviewed_at: string; day: string
}
export interface Stats {
  from: string; to: string; days: DayStat[]; heat: Record<string, number>; heat_from: string
  totals: { reviews: number; new: number; minutes: number; days_studied: number; days: number; per_day: number; seconds_per_card: number }
  retention: number | null; retention_young: number | null; retention_mature: number | null; desired_retention: number
  counts: DeckCounts; forecast: number[]; intervals: { label: string; count: number }[]; hours: HourStat[]
  streak: number; longest_streak: number; lifetime: { reviews: number; duration_ms: number; days: number; notes: number; cards: number; first_day: string }
  hard: StudyCard[]
  sessions: StudySession[]; session_totals: { count: number; seconds: number }
}

export interface TagCount { tag: string; count: number }

export interface Source { id: string; app: 'reader' | 'library' | 'notes'; text: string; note?: string; title: string; url?: string; date?: string; added: boolean; open_url?: string; category?: string }
export interface AICard { kind: 'basic' | 'cloze'; front: string; back: string; extra: string }
