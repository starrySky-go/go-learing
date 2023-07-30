# 1. 简介

本文将介绍 Go 语言中的 `Weighted` 并发原语，包括 `Weighted` 的基本使用方法、实现原理、使用注意事项等内容。能够更好地理解和应用 `Weighted` 来实现资源的管理，从而提高程序的稳定性。

# 2. 问题引入

在微服务架构中，我们的服务节点负责接收其他节点的请求，并提供相应的功能和数据。比如账户服务，其他服务需要获取账户信息，都会通过rpc请求向账户服务发起请求。

这些服务节点通常以集群的方式部署在服务器上，用于处理大量的并发请求。每个服务器都有其处理能力的上限，超过该上限可能导致性能下降甚至崩溃。

在部署服务时，通常会评估服务的并发量，并为其分配适当的资源以处理预期的请求负载。然而，在微服务架构中，存在着上游服务请求下游服务的场景。如果上游服务在某些情况下没有正确考虑并发量，或者由于某些异常情况导致大量请求发送给下游服务，那么下游服务可能面临超过其处理能力的问题。这可能导致下游服务的响应时间增加，甚至无法正常处理请求，进而影响整个系统的稳定性和可用性。下面用一个简单的代码来说明一下:

```go
package main

import (
        "fmt"
        "net/http"
        "sync"
)

func main() {
        // 启动下游服务，用于处理请求
        http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
                // 模拟下游服务的处理逻辑
                // ...
                // 完成请求处理后，从等待组中删除一个等待
                wg.Done()
        })
        // 启动下游服务的 HTTP 服务器
        http.ListenAndServe(":8080", nil)

}
```

这里启动一个简单的HTTP服务器，由其来模拟下游服务，来接收上游服务的请求。下面我们启动一个简单的程序，由其来模拟上游服务发送请求:

```go
func main() {
        // 创建一个等待组，用于等待所有请求完成
        var wg sync.WaitGroup
        // 模拟上游服务发送大量请求给下游服务
        go func() {
                for i := 0; i < 1000000; i++ {
                        wg.Add(1)
                        go sendRequest(&wg)
                }
        }()
        // 等待所有请求完成
        wg.Wait()
}

func sendRequest(wg *sync.WaitGroup) {
        // 模拟上游服务发送请求给下游服务
        resp, err := http.Get("http://localhost:8080/")
        if err != nil {
                fmt.Println("请求失败:", err)
        } else {
                fmt.Println("请求成功:", resp.Status)
        }

        // 请求完成后，通知等待组
        wg.Done()
}
```

这里，我们同时启动了1000000个协程同时往HTTP服务器发送请求，如果服务器配置不够高，亦或者是请求量更多的情况下，已经超过了服务器的处理上限，服务器没有主够的资源去处理这些请求，此时将有可能直接将服务器打挂掉，服务直接不可用。在这种情况下，如果由于上游服务的问题，导致下游服务，甚至整个链路的系统都直接崩溃，这个是不合理的，此时需要有一些手段保护下游服务由于异常流量导致整个系统的崩溃。

这里对上面的场景进行分析，可以发现，此时是由于上游服务大量请求的过来，而当前服务并没有足够的资源去处理这些请求，但是并没有对其加以限制，而是继续处理，最终导致了整个系统的不可用。那么此时就应该进行限流，对并发请求量进行控制，对服务器能够处理的并发数进行合理评估，当并发请求数超过了限制，此时应该直接拒绝其访问，避免整个系统的不可用。

那问题来了，go语言中，有什么方法能够实现资源的管理，如果没有足够的资源，此时将直接返回，不对请求进行处理呢？其实go语言中有`Weighted`类型，在这种场景还挺合适的。下面我们将对其进行介绍。

# 3. 基本使用

### 3.1 基本介绍

`Weighted` 是 Go 语言中 `golang.org/x/sync`包中的一种类型，用于限制并发访问某个资源的数量。它提供了一种机制，允许调用者以不同的权重请求访问资源，并在资源可用时进行授予。

`Weighted`的定义如下，提供了`Acquire`,`TryAcquire`,`Release`三个方法:

```go
type Weighted struct {
   size    int64
   cur     int64
   mu      sync.Mutex
   waiters list.List
}
func (s *Weighted) Acquire(ctx context.Context, n int64) error{}
func (s *Weighted) TryAcquire(n int64) bool{}
func (s *Weighted) Release(n int64) {}
 
```

*   `Acquire`: 以权重 `n` 请求获取资源，阻塞直到资源可用或上下文 `ctx` 结束。
*   `TryAcquire`: 尝试以权重 `n` 获取信号量，如果成功则返回 `true`，否则返回 `false`，并保持信号量不变。
*   `Release`:释放具有权重 `n` 的信号量。

### 3.2 权重说明

有时候，不同请求对资源的消耗是不同的。通过设置权重，你可以更好地控制不同请求对资源的使用情况。例如，某些请求可能需要更多的计算资源或更长的处理时间，你可以设置较高的权重来确保它们能够获取到足够的资源。

其次就是权重大只是代表着请求需要使用到的资源多，对于优先级并不会有作用。在`Weighted` 中，资源的许可是以先进先出（FIFO）的顺序分配的，而不是根据权重来决定获取的优先级。当有多个请求同时等待获取资源时，它们会按照先后顺序依次获取资源的许可。

假设先请求权重为 1 的资源，然后再请求权重为 2 的资源。如果当前可用的资源许可足够满足两个请求的总权重，那么先请求的权重为 1 的资源会先获取到许可，然后是后续请求的权重为 2 的资源。

```go
w.Acquire(context.Background(), 1) // 权重为 1 的请求先获取到资源许可
w.Acquire(context.Background(), 2) // 权重为 2 的请求在权重为 1 的请求之后获取到资源许可
```

### 3.3 基本使用

当使用`Weighted`来控制资源的并发访问时，通常需要以下几个步骤:

*   创建`Weighted`实例，定义好最大资源数
*   当需要资源时，调用`Acquire`方法占据资源
*   当处理完成之后，调用`Release`方法释放资源

下面是一个简单的代码的示例，展示了如何使用`Weighted`实现资源控制：

```go
func main() {
   // 1. 创建一个信号量实例，设置最大并发数
   sem := semaphore.NewWeighted(10)

   // 具体处理请求的函数
   handleRequest := func(id int) {
      // 2. 调用Acquire尝试获取资源
      err := sem.Acquire(context.Background(), 1)
      if err != nil {
         fmt.Printf("Goroutine %d failed to acquire resource\n", id)
      }
      // 3. 成功获取资源，使用defer，在任务执行完之后，自动释放资源
      defer sem.Release(1)
      // 执行业务逻辑
      return
   }

   // 模拟并发请求
   for i := 0; i < 20; i++ {
      go handleRequest(i)
   }

   time.Sleep(20 * time.Second)
}
```

首先，调用`NewWeighted`方法创建一个信号量实例，设置最大并发数为10。然后在每次请求处理前调用`Acquire`方法尝试获取资源，成功获取资源后，使用`defer`关键字，在任务执行完后自动释放资源，调用`Release`方法释放一个资源。

保证最多同时有10个协程获取资源。如果有更多的协程尝试获取资源，它们会等待其他协程释放资源后再进行获取。

# 4. 实现原理

### 4.1 设计初衷

`Weighted`类型的设计初衷是为了在并发环境中实现对资源的控制和限制。它提供了一种简单而有效的机制，允许在同一时间内只有一定数量的并发操作可以访问或使用特定的资源。

### 4.2 基本原理

`Weighted`类型的基本实现原理是基于计数信号量的概念。计数信号量是一种用于控制并发访问的同步原语，它维护一个可用资源的计数器。在`Weighted`中，该计数器表示可用的资源数量。

当一个任务需要获取资源时，它会调用`Acquire`方法。该方法首先会检查当前可用资源的数量，如果大于零，则表示有可用资源，并将计数器减一，任务获取到资源，并继续执行。如果当前可用资源的数量为零，则任务会被阻塞，直到有其他任务释放资源。

当一个任务完成对资源的使用后，它会调用`Release`方法来释放资源。该方法会将计数器加一，表示资源已经可用，其他被阻塞的任务可以继续获取资源并执行。

通过这种方式，`Weighted`实现了对资源的限制和控制。它确保在同一时间内只有一定数量的并发任务可以访问资源，超过限制的任务会被阻塞，直到有其他任务释放资源。这样可以有效地避免资源过度使用和竞争，保证系统的稳定性和性能。

### 4.3 代码实现

#### 4.3.1 结构体定义

`Weighted`的结构体定义如下:

```go
type Weighted struct {
   size    int64
   cur     int64
   mu      sync.Mutex
   waiters list.List
}
```

*   `size`：表示资源的总数量，即可以同时获取的最大资源数量。
*   `cur`：表示当前已经被获取的资源数量。
*   `mu`：用于保护`Weighted`类型的互斥锁，确保并发安全性。
*   `waiters`：使用双向链表来存储等待获取资源的任务。

#### 4.3.2 Acquire方法

`Acquire`方法将获取指定数量的资源。如果当前可用资源数量不足，调用此方法的任务将被阻塞，并加入到等待队列中。

```go
func (s *Weighted) Acquire(ctx context.Context, n int64) error {
   // 1. 使用互斥锁s.mu对Weighted类型进行加锁，确保并发安全性。
   s.mu.Lock()
   // size - cur 代表剩余可用资源数，如果大于请求资源数n, 此时代表剩余可用资源 大于 需要的资源数
   // 其次，Weighted资源分配的顺序是FIFO,如果等待队列不为空，当前请求就需要自动放到队列最后面
   if s.size-s.cur >= n && s.waiters.Len() == 0 {
      s.cur += n
      s.mu.Unlock()
      return nil
   }
    // s.size 代表最大资源数，如果需要的资源数 大于 最大资源数,此时直接返回错误
   if n > s.size {
      // Don't make other Acquire calls block on one that's doomed to fail.
      s.mu.Unlock()
      <-ctx.Done()
      return ctx.Err()
   }
   // 这里代表着当前暂时获取不到资源，此时将创建一个waiter对象放到等待队列最后
   ready := make(chan struct{})
   // waiter对象中包含需要获取的资源数量n和通知通道ready。
   w := waiter{n: n, ready: ready}
   // 将waiter对象放到队列最后
   elem := s.waiters.PushBack(w)
   // 释放锁，让其他请求进来
   s.mu.Unlock()

   select {
   // 如果ctx.Done()通道被关闭，表示上下文已取消，任务需要返回错误。
   case <-ctx.Done():
      err := ctx.Err()
      // 新获取锁，检查是否已经成功获取资源。如果成功获取资源，将错误置为nil，表示获取成功；
      s.mu.Lock()
      select {
      // 通过判断ready channel是否接收到信号，从而来判断是否成功获取资源
      case <-ready:
         err = nil
      default:
         // 判断是否是等待队列中第一个元素
         isFront := s.waiters.Front() == elem
         // 将该请求从等待队列中移除
         s.waiters.Remove(elem)
         // 如果是第一个等待对象，同时还有剩余资源，唤醒后面的waiter。说不定后面的waiter刚好符合条件
         if isFront && s.size > s.cur {
            s.notifyWaiters()
         }
      }
      s.mu.Unlock()
      return err
   // ready通道接收到数据，代表此时已经成功占据到资源了
   case <-ready:
      return nil
   }
}
```

`Weighted`对象用来控制可用资源的数量。它有两个重要的字段，cur和size，分别表示当前可用的资源数量和总共可用的资源数量。

当一个请求通过`Acquire`方法请求资源时，首先会检查剩余资源数量是否足够，并且等待队列中没有其他请求在等待资源。如果满足这两个条件，请求就可以成功获取到资源。

如果剩余资源数量不足以满足请求，那么一个`waiter`的对象会被创建并放入等待队列中。`waiter`对象包含了请求需要的资源数量n和一个用于通知的通道ready。当其他请求调用`Release`方法释放资源时，它们会检查等待队列中的`waiter`对象是否满足资源需求，如果满足，就会将资源分配给该`waiter`对象，并通过`ready`通道来通知它可以执行业务逻辑了。

即使剩余资源数量大于请求所需数量，如果等待队列中存在等待的请求，新的请求也会被放入等待队列中，而不管资源是否足够。这可能导致一些请求长时间等待资源，导致资源的浪费和延迟。因此，在使用`Weighted`进行资源控制时，需要谨慎评估资源配额，并避免资源饥饿的情况发生，以免影响系统的性能和响应能力。

#### 4.3.3 Release方法

`Release`方法将释放指定数量的资源。当资源被释放时，会检查等待队列中的任务。它从队头开始逐个检查等待的元素，并尝试为它们分配资源，直到最后一个不满足资源条件的元素为止。

```go
func (s *Weighted) Release(n int64) {
   // 1. 使用互斥锁s.mu对Weighted类型进行加锁，确保并发安全性。
   s.mu.Lock()
   // 2. 释放资源
   s.cur -= n
   // 3. 异常情况处理
   if s.cur < 0 {
      s.mu.Unlock()
      panic("semaphore: released more than held")
   }
   // 4. 唤醒等待任务
   s.notifyWaiters()
   s.mu.Unlock()
}
```

可以看到，`Release`方法实现相对比较简单，释放资源后，便直接调用`notifyWaiters`方法唤醒处于等待状态的任务。下面来看看`notifyWaiters`方法的具体实现:

```go
func (s *Weighted) notifyWaiters() {
   for {
      // 获取队头元素
      next := s.waiters.Front()
      // 已经没有处于等待状态的协程，此时直接返回
      if next == nil {
         break // No more waiters blocked.
      }

      w := next.Value.(waiter)
      // 如果资源不满足要求 当前waiter的要求，此时直接返回
      if s.size-s.cur < w.n {
         break
      }
      // 否则占据waiter需要的资源数
      s.cur += w.n
      // 移除等待元素
      s.waiters.Remove(next)
      // 唤醒处于等待状态的任务，Acquire方法会 <- ready 来等待信号的到来
      close(w.ready)
   }
}
```

`notifyWaiters`方法会从队头开始获取元素，判断当前资源的剩余数，是否满足`waiter`的要求，如果满足的话，此时先占据该`waiter`需要的资源，之后再将其从等待队列中移除，最后调用`close`方法，唤醒处于等待状态的任务。 之后，再继续队列中取出元素，判断是否满足条件，循环反复，直到不满足`waiter`的条件为止。

#### 4.3.4 TryAcquire方法

`TryAcquire`方法将尝试获取指定数量的资源，但不会阻塞。如果可用资源不足，它会立即返回一个错误，而不是阻塞等待。实现比较简单，只是简单检查当前资源数是否满足要求而已，具体如下:

```go
func (s *Weighted) TryAcquire(n int64) bool {
   s.mu.Lock()
   success := s.size-s.cur >= n && s.waiters.Len() == 0
   if success {
      s.cur += n
   }
   s.mu.Unlock()
   return success
}
```

# 5. 注意事项

#### 5.1 及时释放资源

当使用`Weighted`来管理资源时，确保在使用完资源后，及时调用`Release`方法释放资源。如果不这样做，将会导致资源泄漏，最终导致所有的请求都将无法被处理。下面展示一个简单的代码说明:

```go
package main

import (
        "fmt"
        "sync"
        "time"

        "golang.org/x/sync/semaphore"
)

func main() {
        sem := semaphore.NewWeighted(5) // 创建一个最大并发数为5的Weighted实例
        // 模拟使用资源的任务
        task := func(id int) {
                //1. 成功获取资源
                if err := sem.Acquire(context.Background(), 1); err != nil {
                        fmt.Printf("Task %d failed to acquire resource: %s\n", id, err)
                        return
                }
                // 2. 任务处理完成之后，资源没有被释放
                // defer sem.Release(1) // 使用defer确保在任务完成后释放资源
               
        }

        // 启动多个任务并发执行
        var wg sync.WaitGroup
        for i := 0; i < 10; i++ {
                wg.Add(1)
                go func(id int) {
                        defer wg.Done()
                        task(id)
                }(i)
        }
        wg.Wait() // 等待所有任务完成
}
```

在上面的代码中，我们使用`Weighted`来控制最大并发数为5。我们在任务中没有调用`sem.Release(1)`释放资源，这些资源将一直被占用，后面启动的5个任务将永远无法获取到资源，此时将永远不会继续执行下去。因此，务必在使用完资源后及时调用`Release`方法释放资源，以确保资源的正确回收和释放，保证系统的稳定性和性能。

而且这里最好使用`defer`语句来实现资源的释放，避免`Release`函数在某些异常场景下无法被执行到。

#### 5.2 合理设置并发数

`Weighted`只是提供了一种管理资源的手段，具体的并发数还需要开发人员自行根据系统的实际需求和资源限制，合理设置`Weighted`实例的最大并发数。过大的并发数可能导致资源过度竞争，而过小的并发数可能限制了系统的吞吐量。

具体操作可以到线上预发布环境，不断调整观察，获取到一个最合适的并发数。

#### 5.3 考虑Weighted是否适用于当前场景

`Weighted` 类型可以用于限制并发访问资源的数量，但它也存在一些潜在的缺点，需要根据具体的应用场景和需求权衡利弊。

首先是内存开销，`Weighted` 类型使用一个 `sync.Mutex` 以及一个 `list.List` 来管理等待队列，这可能会占用一定的内存开销。对于大规模的并发处理，特别是在限制极高的情况下，可能会影响系统的内存消耗。

其次是`Weighted` 类型一旦初始化，最大并发数是固定的，无法在运行时动态调整。如果你的应用程序需要根据负载情况动态调整并发限制，可能需要使用其他机制或实现。

而且`Weighted`是严格按照FIFO请求顺序来分配资源的，当某些请求的权重过大时，可能会导致其他请求饥饿，即长时间等待资源。

最后，则是由于 `Weighted` 类型使用了互斥锁来保护共享状态，因此在高并发情况下，争夺锁可能成为性能瓶颈，影响系统的吞吐量。

因此，在使用 `Weighted` 类型时，需要根据具体的应用场景和需求权衡利弊，从而来决定是否使用`Weighted`来实现资源的管理控制。

# 6. 总结

本文介绍了一种解决系统中资源管理问题的解决方案`Weighted`。本文从问题引出，详细介绍了`Weighted`的特点和使用方法。通过了解`Weighted`的设计初衷和实现原理，读者可以更好地理解其工作原理。

同时，文章提供了使用`Weighted`时需要注意的事项，如及时释放资源、合理设置并发数等，从而帮助读者避免潜在的问题，以及能够在比较合适的场景下使用到`Weighted`类型实现资源管理。基于此，我们完成了对`Weighted`的介绍，希望对你有所帮助。你的点赞和收藏将是我最大的动力，比心～
