package main

import (
    "testing"
)

func TestFormatTTLHuman_Basics(t *testing.T) {
    cases := []struct{
        in int64
        want string
    }{
        {0, "0s"},
        {1, "1s"},
        {59, "59s"},
        {60, "1m"},
        {61, "1m 1s"},
        {119, "1m 59s"},
        {3600, "1h"},
        {3661, "1h 1m 1s"},
    }
    for i, c := range cases {
        got := formatTTLHuman(c.in)
        if got != c.want {
            t.Fatalf("case %d: got %q want %q", i, got, c.want)
        }
    }
}

func TestFormatTTLHuman_DaysWeeksMonthsYears(t *testing.T) {
    // Using approximations: 1mo=30d, 1y=365d
    const (
        minute = int64(60)
        hour   = 60 * minute
        day    = 24 * hour
        week   = 7 * day
        month  = 30 * day
        year   = 365 * day
    )

    // 767h36m2s = 31d 23h 36m 2s -> capped to 3 parts
    in := int64(767)*hour + 36*minute + 2
    if got, want := formatTTLHuman(in), "31d 23h 36m"; got != want {
        t.Fatalf("767h36m2s: got %q want %q", got, want)
    }

    // 2 weeks and 3 days
    in = 2*week + 3*day
    if got, want := formatTTLHuman(in), "2w 3d"; got != want {
        t.Fatalf("2w3d: got %q want %q", got, want)
    }

    // 1 year, 2 months, 1 day -> 1y 2mo 1d
    in = year + 2*month + day
    if got, want := formatTTLHuman(in), "1y 2mo 1d"; got != want {
        t.Fatalf("1y2mo1d: got %q want %q", got, want)
    }

    // Less than 1s should not occur (input is seconds), but check negative sentinel
    if got := formatTTLHuman(-1); got != "n/a" {
        t.Fatalf("-1s: got %q want %q", got, "n/a")
    }
}

func TestFormatTTLHuman_CapThreeComponents(t *testing.T) {
    // 1y 2mo 3w 4d -> capped to 3 components: 1y 2mo 3w
    const (
        minute = int64(60)
        hour   = 60 * minute
        day    = 24 * hour
        week   = 7 * day
        month  = 30 * day
        year   = 365 * day
    )
    in := year + 2*month + 3*week + 4*day
    if got, want := formatTTLHuman(in), "1y 2mo 3w"; got != want {
        t.Fatalf("cap3: got %q want %q", got, want)
    }
}
