package jobs

import (
	"testing"
	"time"
)

func TestCronEvery(t *testing.T) {
	sched, dur, err := parseSchedule("@every 30m")
	if err != nil {
		t.Fatal(err)
	}
	if sched != nil || dur != 30*time.Minute {
		t.Fatalf("bad @every: sched=%v dur=%v", sched, dur)
	}
}

func TestCronDaily(t *testing.T) {
	sched, _, err := parseSchedule("@daily")
	if err != nil {
		t.Fatal(err)
	}
	if sched == nil || len(sched.hour) < 1 || contains(sched.hour, 13) {
		t.Fatalf("bad daily schedule: %+v", sched)
	}
}

func TestCronStandard(t *testing.T) {
	sched, _, err := parseSchedule("0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if sched == nil || len(sched.hour) != 1 || sched.hour[0] != 3 {
		t.Fatalf("bad standard schedule: %+v", sched)
	}
	if sched.minute[0] != 0 {
		t.Fatalf("bad minute: %v", sched.minute)
	}
}

func TestCronStepAndRange(t *testing.T) {
	sched, _, err := parseSchedule("*/15 9-17 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	if len(sched.minute) != 4 { // 0,15,30,45
		t.Fatalf("bad minutes: %v", sched.minute)
	}
	if len(sched.hour) != 9 {
		t.Fatalf("bad hours: %v", sched.hour)
	}
	if len(sched.dow) != 5 {
		t.Fatalf("bad dow: %v", sched.dow)
	}
}

func TestNext(t *testing.T) {
	sched, _, err := parseSchedule("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 12, 10, 30, 45, 0, time.UTC)
	next := sched.next(from)
	want := time.Date(2026, 8, 12, 10, 31, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

func TestNextDaily(t *testing.T) {
	sched, _, err := parseSchedule("@daily")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 12, 10, 30, 0, 0, time.UTC)
	next := sched.next(from)
	want := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next daily = %v, want %v", next, want)
	}
}
