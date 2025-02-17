package eventbus

import (
	"fmt"
	"reflect"
	"sync"
)

//Subscriber defines subscription-related bus behavior
type Subscriber interface {
	Subscribe(topic string, fn interface{}) error
	SubscribeAsync(topic string, fn interface{}, transactional bool) error
	SubscribeOnce(topic string, fn interface{}) error
	SubscribeOnceAsync(topic string, fn interface{}) error
	Unsubscribe(topic string, handler interface{}) error
}

//Publisher defines publishing-related bus behavior
type Publisher interface {
	Publish(topic string, args ...interface{})
}

//Controller defines bus control behavior (checking handler's presence, synchronization)
type Controller interface {
	HasCallback(topic string) bool
	WaitAsync()
}

var b *Bus

func init() {
	b = New()
}

// Bus - box for handlers and callbacks.
type Bus struct {
	handlers map[string][]*eventHandler
	lock     sync.Mutex // a lock for the map
	wg       sync.WaitGroup
}

type eventHandler struct {
	callBack      reflect.Value
	flagOnce      bool
	async         bool
	transactional bool
	sync.Mutex    // lock for an event handler - useful for running async callbacks serially
}

// New returns new Bus with empty handlers.
func New() *Bus {
	return &Bus{
		make(map[string][]*eventHandler),
		sync.Mutex{},
		sync.WaitGroup{},
	}
}

// doSubscribe handles the subscription logic and is utilized by the public Subscribe functions
func (bus *Bus) doSubscribe(topic string, fn interface{}, handler *eventHandler) error {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	if !(reflect.TypeOf(fn).Kind() == reflect.Func) {
		return fmt.Errorf("%s is not of type reflect.Func", reflect.TypeOf(fn).Kind())
	}
	bus.handlers[topic] = append(bus.handlers[topic], handler)
	return nil
}

// Subscribe runs Subscribe on package-level bus singleton
func Subscribe(topic string, fn interface{}) error {
	return b.Subscribe(topic, fn)
}

// Subscribe subscribes to a topic.
// Returns error if `fn` is not a function.
func (bus *Bus) Subscribe(topic string, fn interface{}) error {
	return bus.doSubscribe(topic, fn, &eventHandler{
		reflect.ValueOf(fn), false, false, false, sync.Mutex{},
	})
}

// SubscribeAsync runs SubscribeAsync on package-level bus singleton
func SubscribeAsync(topic string, fn interface{}, transactional bool) error {
	return b.SubscribeAsync(topic, fn, transactional)
}

// SubscribeAsync subscribes to a topic with an asynchronous callback
// Transactional determines whether subsequent callbacks for a topic are
// run serially (true) or concurrently (false)
// Returns error if `fn` is not a function.
func (bus *Bus) SubscribeAsync(topic string, fn interface{}, transactional bool) error {
	return bus.doSubscribe(topic, fn, &eventHandler{
		reflect.ValueOf(fn), false, true, transactional, sync.Mutex{},
	})
}

// SubscribeOnce runs SubscribeOnce on package-level bus singleton
func SubscribeOnce(topic string, fn interface{}) error {
	return b.SubscribeOnce(topic, fn)
}

// SubscribeOnce subscribes to a topic once. Handler will be removed after executing.
// Returns error if `fn` is not a function.
func (bus *Bus) SubscribeOnce(topic string, fn interface{}) error {
	return bus.doSubscribe(topic, fn, &eventHandler{
		reflect.ValueOf(fn), true, false, false, sync.Mutex{},
	})
}

// SubscribeOnceAsync runs SubscribeOnceAsync on package-level bus singleton
func SubscribeOnceAsync(topic string, fn interface{}) error {
	return b.SubscribeOnceAsync(topic, fn)
}

// SubscribeOnceAsync subscribes to a topic once with an asynchronous callback
// Handler will be removed after executing.
// Returns error if `fn` is not a function.
func (bus *Bus) SubscribeOnceAsync(topic string, fn interface{}) error {
	return bus.doSubscribe(topic, fn, &eventHandler{
		reflect.ValueOf(fn), true, true, false, sync.Mutex{},
	})
}

// HasCallback runs HasCallback on package-level bus singleton
func HasCallback(topic string) bool {
	return b.HasCallback(topic)
}

// HasCallback returns true if exists any callback subscribed to the topic.
func (bus *Bus) HasCallback(topic string) bool {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	_, ok := bus.handlers[topic]
	if ok {
		return len(bus.handlers[topic]) > 0
	}
	return false
}

// Unsubscribe runs Unsubscribe on package-level bus singleton
func Unsubscribe(topic string, handler interface{}) error {
	return b.Unsubscribe(topic, handler)
}

// Unsubscribe removes callback defined for a topic.
// Returns error if there are no callbacks subscribed to the topic.
func (bus *Bus) Unsubscribe(topic string, handler interface{}) error {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	if _, ok := bus.handlers[topic]; ok && len(bus.handlers[topic]) > 0 {
		bus.removeHandler(topic, bus.findHandlerIdx(topic, reflect.ValueOf(handler)))
		return nil
	}
	return fmt.Errorf("topic %s doesn't exist", topic)
}

// Publish runs Publish on package-level bus singleton
func Publish(topic string, args ...interface{}) {
	b.Publish(topic, args...)
}

// Publish executes callback defined for a topic. Any additional argument will be transferred to the callback.
func (bus *Bus) Publish(topic string, args ...interface{}) {
	bus.lock.Lock() // will unlock if handler is not found or always after setUpPublish
	defer bus.lock.Unlock()
	if handlers, ok := bus.handlers[topic]; ok {
		for i, handler := range handlers {
			if handler.flagOnce {
				bus.removeHandler(topic, i)
			}
			if !handler.async {
				bus.doPublish(handler, topic, args...)
			} else {
				bus.wg.Add(1)
				if handler.transactional {
					handler.Lock()
				}
				go bus.doPublishAsync(handler, topic, args...)
			}
		}
	}
}

func (bus *Bus) doPublish(handler *eventHandler, topic string, args ...interface{}) {
	passedArguments := bus.setUpPublish(topic, args...)
	handler.callBack.Call(passedArguments)
}

func (bus *Bus) doPublishAsync(handler *eventHandler, topic string, args ...interface{}) {
	defer bus.wg.Done()
	if handler.transactional {
		defer handler.Unlock()
	}
	bus.doPublish(handler, topic, args...)
}

func (bus *Bus) removeHandler(topic string, idx int) {
	if _, ok := bus.handlers[topic]; !ok {
		return
	}
	l := len(bus.handlers[topic])

	copy(bus.handlers[topic][idx:], bus.handlers[topic][idx+1:])
	bus.handlers[topic][l-1] = nil // or the zero value of T
	bus.handlers[topic] = bus.handlers[topic][:l-1]
}

func (bus *Bus) findHandlerIdx(topic string, callback reflect.Value) int {
	if _, ok := bus.handlers[topic]; ok {
		for idx, handler := range bus.handlers[topic] {
			if handler.callBack == callback || handler.callBack.Pointer() == callback.Pointer() {
				return idx
			}
		}
	}
	return -1
}

func (bus *Bus) setUpPublish(topic string, args ...interface{}) []reflect.Value {

	passedArguments := make([]reflect.Value, 0, len(args))
	for _, arg := range args {
		passedArguments = append(passedArguments, reflect.ValueOf(arg))
	}
	return passedArguments
}

// WaitAsync runs WaitAsync on package-level bus singleton
func WaitAsync() {
	b.WaitAsync()
}

// WaitAsync waits for all async callbacks to complete
func (bus *Bus) WaitAsync() {
	bus.wg.Wait()
}
