package scheduled

import "github.com/CorneeldH/lighthouse/internal/cron"

func slotOf(d Due) string { return cron.Slot(d.Slot) }

type fakeJob struct{ Job }

func (*fakeJob) Name() string { return "fake" }
