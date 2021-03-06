package remote

import (
	"sync"
	"sync/atomic"

	"github.com/aergoio/aergo-actor/mailbox"

	"github.com/aergoio/aergo-actor/actor"
	"github.com/aergoio/aergo-actor/eventstream"
)

var endpointManager *endpointManagerValue

type endpointLazy struct {
	valueFunc func() *endpoint
	unloaded  uint32
}

type endpoint struct {
	writer  *actor.PID
	watcher *actor.PID
}

type endpointManagerValue struct {
	connections        *sync.Map
	config             *remoteConfig
	endpointSupervisor *actor.PID
	endpointSub        *eventstream.Subscription
}

func startEndpointManager(config *remoteConfig) {
	plog.Debug().Msg("Started EndpointManager")

	props := actor.FromProducer(newEndpointSupervisor).
		WithGuardian(actor.RestartingSupervisorStrategy()).
		WithSupervisor(actor.RestartingSupervisorStrategy()).
		WithDispatcher(mailbox.NewSynchronizedDispatcher(300))
	endpointSupervisor, _ := actor.SpawnNamed(props, "EndpointSupervisor")

	endpointManager = &endpointManagerValue{
		connections:        &sync.Map{},
		config:             config,
		endpointSupervisor: endpointSupervisor,
	}

	endpointManager.endpointSub = eventstream.
		Subscribe(endpointManager.endpointEvent).
		WithPredicate(func(m interface{}) bool {
			switch m.(type) {
			case *EndpointTerminatedEvent, *EndpointConnectedEvent:
				return true
			}
			return false
		})
}

func stopEndpointManager() {
	eventstream.Unsubscribe(endpointManager.endpointSub)
	endpointManager.endpointSupervisor.GracefulStop()
	endpointManager.endpointSub = nil
	endpointManager.connections = nil
	plog.Debug().Msg("Stopped EndpointManager")
}

func (em *endpointManagerValue) endpointEvent(evn interface{}) {
	switch msg := evn.(type) {
	case *EndpointTerminatedEvent:
		em.removeEndpoint(msg)
	case *EndpointConnectedEvent:
		endpoint := em.ensureConnected(msg.Address)
		endpoint.watcher.Tell(msg)
	}
}

func (em *endpointManagerValue) remoteTerminate(msg *remoteTerminate) {
	address := msg.Watchee.Address
	endpoint := em.ensureConnected(address)
	endpoint.watcher.Tell(msg)
}

func (em *endpointManagerValue) remoteWatch(msg *remoteWatch) {
	address := msg.Watchee.Address
	endpoint := em.ensureConnected(address)
	endpoint.watcher.Tell(msg)
}

func (em *endpointManagerValue) remoteUnwatch(msg *remoteUnwatch) {
	address := msg.Watchee.Address
	endpoint := em.ensureConnected(address)
	endpoint.watcher.Tell(msg)
}

func (em *endpointManagerValue) remoteDeliver(msg *remoteDeliver) {
	address := msg.target.Address
	endpoint := em.ensureConnected(address)
	endpoint.writer.Tell(msg)
}

func (em *endpointManagerValue) ensureConnected(address string) *endpoint {
	e, ok := em.connections.Load(address)
	if !ok {
		el := &endpointLazy{}
		var once sync.Once
		el.valueFunc = func() *endpoint {
			once.Do(func() {
				rst, _ := em.endpointSupervisor.RequestFuture(address, -1).Result()
				ep := rst.(*endpoint)
				el.valueFunc = func() *endpoint {
					return ep
				}
			})
			return el.valueFunc()
		}
		e, _ = em.connections.LoadOrStore(address, el)
	}

	el := e.(*endpointLazy)
	return el.valueFunc()
}

func (em *endpointManagerValue) removeEndpoint(msg *EndpointTerminatedEvent) {
	v, ok := em.connections.Load(msg.Address)
	if ok {
		le := v.(*endpointLazy)
		if atomic.CompareAndSwapUint32(&le.unloaded, 0, 1) {
			em.connections.Delete(msg.Address)
			ep := le.valueFunc()
			ep.watcher.Tell(msg)
			ep.writer.Tell(msg)
		}
	}
}

type endpointSupervisor struct{}

func newEndpointSupervisor() actor.Actor {
	return &endpointSupervisor{}
}

func (state *endpointSupervisor) Receive(ctx actor.Context) {
	if address, ok := ctx.Message().(string); ok {
		e := &endpoint{
			writer:  state.spawnEndpointWriter(address, ctx),
			watcher: state.spawnEndpointWatcher(address, ctx),
		}
		ctx.Respond(e)
	}
}

func (state *endpointSupervisor) HandleFailure(supervisor actor.Supervisor, child *actor.PID, rs *actor.RestartStatistics, reason interface{}, message interface{}) {
	supervisor.RestartChildren(child)
}

func (state *endpointSupervisor) spawnEndpointWriter(address string, ctx actor.Context) *actor.PID {
	props := actor.
		FromProducer(newEndpointWriter(address, endpointManager.config)).
		WithMailbox(newEndpointWriterMailbox(endpointManager.config.endpointWriterBatchSize, endpointManager.config.endpointWriterQueueSize))
	pid := ctx.Spawn(props)
	return pid
}

func (state *endpointSupervisor) spawnEndpointWatcher(address string, ctx actor.Context) *actor.PID {
	props := actor.
		FromProducer(newEndpointWatcher(address))
	pid := ctx.Spawn(props)
	return pid
}
