# 1. 简介
我们在 [Sync.Pool: 提高go语言程序性能的关键一步](./Sync.Pool: 提高go语言程序性能的关键一步.md) 一文中，已经了解了使用`sync.Pool`来实现对象的复用以减少对象的频繁创建和销毁，以及使用`sync.Pool`的一些常见注意事项。

在这篇文章中，我们将剖析`sync.Pool`内部实现中，介绍了`sync.Pool`比较巧妙的内部设计思路以及其实现方式。在这个过程中，也间接介绍了为何不加锁也能够实现线程安全。

主要会涉及到Go语言中实现并发的GMP模型以及其基本的调度原理，以及本地缓存的设计，无锁队列的使用这几个部分的内容，综上这几个方面的内容实现了不加锁也能够保证线程安全。


# 2. GMP之间的绑定关系
为了之能够帮助我们后续更好得理解`sync.Pool`的设计与实现，这里需要对GMP模型进行简单的介绍。GMP模型是Go语言中的一种协作式调度模型，其中G表示Goroutine，M可以理解为内核线程，P为逻辑处理器，简单理解其维护了一条Goroutine队列。

### 2.1 M和P的关系
在GMP模型中，M和P可以动态绑定，一个M可以在运行时绑定到任意一个P上，而一个P也可以与任意一个M绑定。这种绑定方式是动态的，可以根据实际情况进行灵活调整，从而实现更加高效的协程调度。

尽管M和P可以动态绑定，但在特定时间点，一个M只会对应一个P。这是因为M是操作系统线程，而P是Go语言的逻辑处理器，Go语言的逻辑处理器需要在某个操作系统线程中运行，并且是被该逻辑处理器(P)单独占用的。

P的数量一般是和CPU核数保持一致，每个P占用一个CPU核心来执行，可以通过runtime.GOMAXPROCS函数来修改。不过在大多数情况下，不需要手动修改，Go语言的调度器会根据实际情况自动进行调整。

### 2.2 P和G的关系
刚创建的Goroutine会被放入当前线程对应P的本地队列中等待被执行。如果本地队列已满，则会放入全局队列中，供其他线程的P来抢占执行。

当P空闲时，会尝试从全局队列中获取Goroutine来执行。如果全局队列中没有Goroutine，则会从其他处理器的本地运行队列中"偷取"一些Goroutine来执行。

如果协程执行过程中遇到阻塞操作（比如等待I/O或者锁），处理器（P）会立即将协程移出本地运行队列，并执行其他协程，直到被阻塞的协程可以继续执行为止。被阻塞的协程会被放到相应的等待队列中等待事件发生后再次被唤醒并加入到运行队列中，但不一定还是放回原来的处理器(P)的等待队列中。

从上述过程可以看出，G和P的绑定关系是动态绑定的，在不同的时间点，同一个G可能在不同的P上执行，同时，在不同的时间点，P也会调度执行不同的G。

### 2.3 总结

每个P在某个时刻只能绑定一个M，而每个G在某个时刻也只存在于某个P的等待队列中，等待被调度执行。这是GMP模型的基本调度原理，也是Go语言高效调度的核心所在。通过动态绑定和灵活调度，可以充分利用多核处理器的计算能力，从而实现高并发、高效率的协程调度。

通过对GMP模型的基本了解，能够帮助我们后续更好得理解`sync.Pool`的设计与实现。

# 3.Sync.Pool与GMP模型

### 3.1 sync.Pool性能问题

这里我们回到`sync.Pool`, 可以简单使用切片，存储可复用的对象，在需要时从中取出对象，用完之后再重新放回池子中，实现对象的重复使用。

当多个协程同时从 `sync.Pool` 中取对象时，会存在并发问题，因此需要实现并发安全。一种简单的实现方式是加锁，每个协程在取数据前先加锁，然后获取数据，再解锁，实现串行读取的效果。但是这种方式在并发比较大的场景下容易导致大量协程进入阻塞状态，从而进一步降低性能。

因此，为了提高程序的性能，我们需要寻找一种减少并发冲突的方式。有什么方式能够减少并发冲突呢?

### 3.2 基于GMP模型的改进

回到GMP模型，从第二节对GMP模型的介绍中，我们知道协程(G)需要在逻辑处理器(P)上执行，而逻辑处理器的数量是有限的，一般与CPU核心数相同。而之前的sync.Pool实现方式是所有P竞争同一份数据，容易导致大量协程进入阻塞状态，影响程序性能。

那我们这里，是不是能够将 `sync.Pool` 分成多个小的存储池，每个P都用拥有一个小的存储池呢? 在每个小存储池中分别使用独立的锁进行并发控制。这样可以避免多个协程同时竞争同一个全局锁的情况，降低锁的粒度，从而减少并发冲突。

协程运行时都需要绑定一个逻辑处理器(P)，此时每个P都有自己的数据缓存，需要对象时从绑定的P的缓存中获取，用完后重新放回。这种实现方式减少了协程竞争同一份数据的情况，只有在同一个逻辑处理器上的协程才存在竞争，从而减少并发冲突，提升性能。


### 3.3 能不能完全不加锁

在上面的实现中，处于不同的P上的协程都是操作不同的数据，此时并不会出现并发问题。唯一可能出现并发问题的地方，为协程在获取缓存对象时，逻辑处理器中途调度其他协程来执行，此时才可能导致的并发问题。那这里能不能避免并发呢?

那如果能够将协程固定到逻辑处理器P上，并且不允许被抢占，也就是该P上永远都是执行某一个协程，直到成功获取缓存对象后，才允许逻辑处理器去调度执行其他协程，那么就可以完全避免并发冲突的问题了。

因此，如果我们能够做到协程在读取缓冲池中的数据时，能够完全占用逻辑处理器P，不会被抢占，此时就不会出现并发了，也不需要加锁了。

幸运的是，runtime包中提供了`runtime_procPin`调用，可以将当前协程固定到协程所在逻辑处理器P上，并且不允许被抢占，也就是逻辑处理器P一直都被当前协程所独享。在获取缓存对象时，我们可以使用`runtime_procPin`将当前协程固定到逻辑处理器P上，然后从该逻辑处理器P的缓存中获取对象。这样做不仅可以避免并发冲突，还可以避免上下文切换和锁竞争等性能问题。


# 4. sync.Pool初步实现
下面来看看当前`sync.Pool`的部分代码，其原理便是上述所提到的方式。具体来说，每个逻辑处理器P保存一份数据，并利用`runtime_procPin`来避免同一逻辑处理器P中的协程发生并发冲突。

需要注意的是，下面所展示的代码只是部分代码，并不包含完整的实现。但是这些代码涵盖了前面所提到的实现方式。同时，为了讲述方便，也修改部分实现，后文会说明当前`sync.Pool`当前真正的实现。

### 4.1 sync.Pool结构体定义
```go
type Pool struct {
   // 指向 poolLocal 结构体切片的地址，长度与cpu核心数保持一致
   local     unsafe.Pointer // local fixed-size per-P pool, actual type is [P]poolLocal
   // 记录当前 poolLocal 切片的长度
   localSize uintptr        // size of the local array
   // New optionally specifies a function to generate
   // a value when Get would otherwise return nil.
   // It may not be changed concurrently with calls to Get.
   New func() any
}

type poolLocal struct {
   // 上文所说的小缓冲池的实现
   private []any       // Can be used only by the respective P.
}
```
其中，`New` 函数用于创建一个新的对象，以便向池中添加对象。当池中没有可用对象时，会调用该函数。`local` 是指向 `poolLocal` 切片的指针，`poolLocal` 即为上文提到的小缓冲池，每个逻辑处理器都有一个。

### 4.2 Put方法
```go
func (p *Pool) Put(x any) {
   if x == nil {
      return
   }
   // 这里调用pin方法,获取到poolLocal
   l, _ := p.pin()
   // 将对象重新放入逻辑处理的小缓冲池当中
   l.private = l.private.append(x)
   // 这个为解除 Proccssor的固定，Processor能够调度其他协程去执行
   runtime_procUnpin()
}
```
`Put`方法是用于将对象重新放入缓冲池，首先调用`pin`方法获取到`poolLocal`,然后将对象放入到`poolLocal`当中，然后再通过`runtime_procUnpin`调用，解除对当前P的绑定。

可以发现，其中比较重要的逻辑，是调用`pin`方法获取到P对应的`poolLocal`,下面我们来看`pin`方法的实现。
```go
func (p *Pool) pin() (*poolLocal, int) {
    // 调用runtime_procPin,占用Processor,不会被抢占
   pid := runtime_procPin()
   // 获取localSize字段的值
   s := runtime_LoadAcquintptr(&p.localSize) // load-acquire
   // 获取poolLocal切片
   l := p.local                              // load-consume
   // pid为协程编号，如果pid < localSize的值，说明属于该processor的缓冲池已经创建好了
   if uintptr(pid) < s {
      // 根据pid获取对应的缓冲池
      return indexLocal(l, pid), pid
   }
   // 否则走下面逻辑
   return p.pinSlow()
}
func indexLocal(l unsafe.Pointer, i int) *poolLocal {
   // 直接通过Processor的编号，计算出偏移量获取到对应的poolLocal
   lp := unsafe.Pointer(uintptr(l) + uintptr(i)*unsafe.Sizeof(poolLocal{}))
   return (*poolLocal)(lp)
}
```
在函数开始时，`pin`方法通过调用`runtime_procPin`方法，占用当前goroutine所在的P，不允许其他goroutine抢占该P，这里能够避免处于同一等待队列中的协程出现并发读取`poolLocal`的数据的问题。

同时`runtime_procPin`方法也会返回当前P的编号，在系统内部是唯一的，从0开始依次递增的整数。其将能够作为`poolLocal`的切片下标，来读取`poolLocal`。

接下来，通过原子操作`runtime_LoadAcquintptr`读取`localSize`字段的值，该字段表示当前poolLocal实例切片的长度。如果当前的P编号小于`localSize`的值，则表示该P的`poolLocal`实例已经被创建，可以直接获取该P对应的`poolLocal`实例并返回。

如果P编号大于等于`localSize`的值，此时说明该P对应的`poolLocal`还没创建，通过调用`pinSlow()`方法进行初始化。下面继续来看`pinSlow`方法的具体实现。

```go
func (p *Pool) pinSlow(pid int) (*poolLocal, int) {
   // 获取processor数量
   size := runtime.GOMAXPROCS(0)
   // 创建一个新的poolLocal切片
   local := make([]poolLocal, size)
   // 将切片地址存储到local字段当中
   atomic.StorePointer(&p.local, unsafe.Pointer(&local[0])) // store-release
   // 将切片数组长度 存储到 localSize 当中
   runtime_StoreReluintptr(&p.localSize, uintptr(size))     // store-release
   // 根据pid,获取到对应的poolLocal
   return &local[pid], pid
}
```
首先调用 `runtime.GOMAXPROCS(0)` 获取当前程序可用的 processor 数量，并基于这个数量创建了一个新的 `poolLocal` 切片 `local`，这里也印证了我们之前所说的，每一个Processor都有一个小缓冲池。

接着，使用原子操作 `atomic.StorePointer` 将指向 `p.local` 的指针修改为指向新创建的 `local` 切片的第一个元素的指针。然后使用 `runtime_StoreReluintptr` 原子操作将 `p.localSize` 修改为 `size`。到此为止，便完成了`poolLocal`切片的初始化操作。

最后返回当前 processor 对应的 `poolLocal` 指针和它的编号 `pid`。由于这里新创建的 `local` 切片是局部变量，在 `pinSlow` 函数返回后，它就无法被访问了。但是，由于我们已经将 `p.local` 修改为指向这个切片的第一个元素的指针，所以其他 processor在调用 `pin` 方法时，就能获取到新创建的 `poolLocal`。


### 4.3 Get方法
```go
func (p *Pool) Get() any {
   l, pid := p.pin()
   var x any
   if n := len(l.private); n > 0 {
       x = l.private[n-1]
       l.private[n-1] = nil // Just to be safe
       l.private = l.private[:n-1]
   }
   runtime_procUnpin()
   if x == nil && p.New != nil {
      x = p.New()
   }
   return x
}
```
首先调用`pin()`方法，获得当前协程绑定的缓存池`local`和协程编号`pid`。接着从`local.private`中尝试取出对象，如果取出来是空，说明缓冲池中没有对象，此时调用`runtime_procUnpin()`方法解绑协程与处理器的绑定。

如果没有从缓冲池中成功获取对象，并且`Pool`结构体的`New`字段非空，则调用`New`字段所指向的函数创建一个新对象并返回。


### 4.4 总结
到此为止，`sync.Pool`已经通过结合GMP模型的特点，给每一个P设置一份缓存数据，当逻辑处理器上的协程需要从`sync.Pool`获取可重用对象时，此时将从逻辑处理器P对应的缓存中取出对象，避免了不同逻辑处理器的竞争。

此外，也通过调用`runtime_procPin`方法，让协程能够在某段时间独占该逻辑处理器，避免了锁的竞争和不必要的上下文切换的消耗，从而提升性能。

# 5. sync.Pool实现优化
### 5.1 问题描述
在`sync.Pool`的初步实现中，我们让每一个逻辑处理器P都拥有一个小缓冲池，让各个逻辑处理器P上的协程从`sync.Pool`获取对象时不会竞争，从而提升性能。

现在可能存在的问题，在于每个 Processor 都保存一份缓存数据，那么当某个 Processor 上的 goroutine 需要使用缓存时，可能会发现它所在的 Processor 上的缓存池为空的，而其他 Processor 上的缓存对象却没有被利用。这样就浪费了其他 Processor 上的资源。

回到`sync.Pool`的设计初衷来看，首先是提升程序性能，减少重复创建和销毁对象的开销；其次是减少内存压力，通过对象复用，从而降低程序GC频次。从这两个方面来看，上面`sync.Pool`的初步实现其实存在一些优化空间的。

这里就陷入了一个两难的境地，如果多个Processor共享同一个缓冲池，会存在容易导致大量协程进入阻塞状态，进一步降低性能。每个 Processor 都保存一份缓存数据的话，此时也容易陷入资源浪费的问题。那能怎么办呢?


### 5.2 实现优化
很多时候，可能并没有十全十美的事情，我们往往需要折中。比如上面多个Processor共享同一个缓冲池，会降低性能；而每个 Processor 都保存一份缓存数据的话，容易陷入资源浪费的问题。

这个时候，我们可以折中一下，不采用完全共享的模式，也不采用完全独占的模式。而**采用部分独有、部分共享**的模式。每个 Processor 独占一部分缓存，可以避免不同 Processor 之间的竞争，提高并发性能。同时，每个 Processor 也可以共享其他 Processor 上的缓存，避免了浪费。相对于完全共享和完全独立的模式，这种设计方式是不是能够更好地平衡并发性能和缓存利用效率。

同时，也可以基于部分独有，部分共享的模式的基础上，再对其进行优化。对于共享部分的资源，可以使用多个缓冲池来存储，是将其给了所有的Processor，每个Processor保留一部分共享数据。

当Processor读取数据时，此时先从自身的私有缓冲中读取，读取不到再到自身的共享缓存中读取，读取不到才到其他Processor读取其共享部分。这样子能够避免了多个Processor同时竞争一个池导致的性能问题。同时，共享部分也可以被充分利用，避免了资源浪费。

# 6.Sync.Pool最终实现
### 6.1 sync.Pool结构体定义
```go
type Pool struct {
   noCopy noCopy
   // 1. 指向poolLocal切片的指针
   local     unsafe.Pointer // local fixed-size per-P pool, actual type is [P]poolLocal
   // 2. 对应local切片的长度
   localSize uintptr        // size of the local array
   // 3. 缓存池中没对象时，调用设置的New函数来创建对象
   New func() any
   // 部分与此次讲述无关内容,未被包含进来
   // ....
}

// 每个Processor都会对应一个poolLocal
type poolLocal struct {
   // 存储缓存对象的数据结构
   poolLocalInternal
   // 用于内存对齐
   pad [128 - unsafe.Sizeof(poolLocalInternal{})%128]byte
}

// Local per-P Pool appendix.
type poolLocalInternal struct {
   // 存储每个Processor独享的对象
   private any       // Can be used only by the respective P.
   // 存储Processor共享的对象，这个是一个无锁队列
   shared  poolChain // Local P can pushHead/popHead; any P can popTail.
}
```
首先说明`poolLocal`结构体，可以认为是一个小缓冲池，每个Processor都会有对应的`poolLocal`对象。`poolLocal`中对象的存储通过`poolLocalInternal`来实现，至于`poolLocal`中的`pad`字段只是用于内存对其。

`poolLocalInternal`其中包含`private`字段和`shared`字段，`private`字段保存了上文所说的Processor独占的缓存对象，而`shared`字段，也就是我们上文所说的共享缓冲池组成的一部分，是允许Processor之间相互读取的。`shared`字段的类型为`poolChain`，是一个无锁队列，调用`pushHead`能够将数据放入共享缓冲池，调用`popHead`能够从缓冲池中取出数据，无需加锁也是并发安全的，这个并非今日的重点，在此简单描述一下。

`Pool`结构体中`local`字段，指向了`poolLocal`结构体切片的地址，而`localSize`字段的值，为前面`poolLocal`切片的长度。
### 6.2 Get方法
```go
func (p *Pool) Get() any {
   l, pid := p.pin()
   x := l.private
   l.private = nil
   if x == nil {
      // Try to pop the head of the local shard. We prefer
      // the head over the tail for temporal locality of
      // reuse.
      x, _ = l.shared.popHead()
      if x == nil {
         x = p.getSlow(pid)
      }
   }
   runtime_procUnpin()
   if x == nil && p.New != nil {
      x = p.New()
   }
   return x
}
```
首先调用`pin()`方法，获取当前Processor对应的`poolLocal`对象和协程编号`pid`，同时占用该Processor。

开始尝试获取对象，首先从`poolLocal`对象中获取私有缓存`private`。如果私有缓存为空，则尝试从共享缓存`shared`的头部弹出一个元素`x`，并赋值给`x`。如果共享缓存也为空，则调用`getSlow()`方法从其他Processor的共享缓存或`New`方法中获取元素`x`。释放当前Processor的占用。如果元素`x`不为空，则返回`x`，否则如果`New`方法不为空，则调用`New`方法生成一个新的元素`x`并返回，否则返回`nil`。

可以看出来，在`Get`方法的外层，主要是尝试从Proessor对应的`poolLocal`中获取数据，读取不到，则调用`getSlow`方法，尝试从其他Processor的共享数据中获取。下面来看`getSlow`方法的逻辑:
```go
func (p *Pool) getSlow(pid int) any {
   // See the comment in pin regarding ordering of the loads.
   size := runtime_LoadAcquintptr(&p.localSize) // load-acquire
   locals := p.local                            // load-consume
   // Try to steal one element from other procs.
   for i := 0; i < int(size); i++ {
      // 获取poolLocal
      l := indexLocal(locals, (pid+i+1)%int(size))
      // poolLocal中的shared是一个无锁队列，无需加锁，也能够保证线程安全
      if x, _ := l.shared.popTail(); x != nil {
         return x
      }
   }
   // 与sync.Pool对象回收的相关逻辑先删除,与此次讲述并无太大关系
   // ....

   return nil
}
```
`getSlow`方法实现较为简单，首先读取`Pool`结构体中`localSize`字段的值，得知当前有多少个`poolLocal`。然后对所有的`poolLocal`进行遍历，尝试从其他`poolLocal`的共享缓存中获取数据，成功获取则直接返回。

### 6.3 Put方法
```go
func (p *Pool) Put(x any) {
   if x == nil {
      return
   }
   l, _ := p.pin()
   if l.private == nil {
      l.private = x
      x = nil
   }
   if x != nil {
      l.shared.pushHead(x)
   }
   runtime_procUnpin()
}
```
首先调用`pin`方法，获取当前Processor对应的`poolLocal`，然后将x放到该`poolLocal`的`private`字段中，也就是放到当前Processor的私有缓存中。如果`private`字段不为空，说明已经有对象放到`private`中了，那么x则会放到`poolLocal`的`shared`字段中，通过无锁队列的方式加入到共享资源池中。

### 6.4 总结
到此为止，在`sync.Pool`原本的实现上，对缓存数据的设计进行了优化，将缓存数据中区分为私有缓存部分和共享部分。此时在一定程度上避免不同 Processor 之间的竞争，提高并发性能。同时，每个 Processor 也可以共享其他 Processor 上的缓存，避免了内存的浪费。

# 7.总结
这篇文章，我们其实主要介绍了`sync.Pool`的实现原理。

我们首先基于GMP模型完成`sync.Pool`的一个实现，基于该实现，引出了**部分独有、部分共享**的模式的优化。在这个过程中，也展示了`sync.Pool`的部分源码，以便能够更好得理解`sync.Pool`的实现。

同时，基于实现的讲述，我们也间接得解答了`sync.Pool`为何不需要加锁也保证了线程安全的问题。

这次讲述`sync.Pool`的实现过程中，并没有直接讲述`sync.Pool`源码的实现，而是一步一步得对现有实现进行优化，将其中比较好的点给描述出来，希望能够有所帮助。
