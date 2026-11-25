// Formats of the telemetry measures. Durations arrive from the API in seconds.
export const percent = (v: number): string => `${Math.round(v * 100)}%`
export const hours = (seconds: number): string => `${(seconds / 3600).toFixed(1)} h`
export const money = (v: number): string => v.toFixed(2)
