package plugin

import (
	"testing"
	"time"

	"github.com/aergoio/aergo-actor/actor"
	"github.com/stretchr/testify/assert"
)

type SmartActor struct {
	PassivationHolder
}

func (state *SmartActor) Receive(context actor.Context) {
	switch context.Message().(type) {
	}
}

func TestPassivation(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	UnitOfTime := time.Duration(200 * time.Millisecond)
	PassivationDuration := time.Duration(3 * UnitOfTime)
	props := actor.
		FromProducer(func() actor.Actor { return &SmartActor{} }).
		WithMiddleware(Use(&PassivationPlugin{Duration: PassivationDuration}))

	pid := actor.Spawn(props)
	time.Sleep(UnitOfTime)
	time.Sleep(UnitOfTime)
	{
		_, found := actor.ProcessRegistry.LocalPIDs.Get(pid.Id)
		assert.True(t, found)
	}
	pid.Tell("keepalive")
	time.Sleep(UnitOfTime)
	time.Sleep(UnitOfTime)
	{
		_, found := actor.ProcessRegistry.LocalPIDs.Get(pid.Id)
		assert.True(t, found)
	}
	time.Sleep(UnitOfTime)
	time.Sleep(UnitOfTime)
	{
		_, found := actor.ProcessRegistry.LocalPIDs.Get(pid.Id)
		assert.False(t, found)
	}
}
