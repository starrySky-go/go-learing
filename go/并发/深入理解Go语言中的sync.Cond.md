# 1. 简介
本文将介绍 Go 语言中的 ` sync.Cond  `并发原语，包括 `sync.Cond`的基本使用方法、实现原理、使用注意事项以及常见的使用使用场景。能够更好地理解和应用 Cond 来实现 goroutine 之间的阻塞等待。

# 2. 基本使用
### 2.1 定义
`sync.Cond`是Go语言标准库中的一个类型，代表条件变量。条件变量是用于多个goroutine之间进行阻塞等待的一种机制。`sync.Cond`可以用于等待和通知goroutine，以便它们可以在特定条件下等待或继续执行。

### 2.2 方法说明
`sync.Cond`的定义如下，提供了`Wait` ,`Singal`,`Broadcast`以及`NewCond`方法
```go
type Cond struct {
   noCopy noCopy
   // L is held while observing or changing the condition
   L Locker

   notify  notifyList
   checker copyChecker
}

func NewCond(l Locker) *Cond {}
func (c *Cond) Wait() {}
func (c *Cond) Signal() {}
func (c *Cond) Broadcast() {}
```
- `NewCond`方法： 提供创建`Cond`实例的方法
- `Wait`方法: 使当前线程进入阻塞状态，等待其他协程唤醒
- `Singal`方法: 唤醒一个等待该条件变量的线程，如果没有线程在等待，则该方法会立即返回。
- `Broadcast`方法: 唤醒所有等待该条件变量的线程，如果没有线程在等待，则该方法会立即返回。


### 2.3 使用方式
当使用`sync.Cond`时，通常需要以下几个步骤：

- 定义一个互斥锁，用于保护共享数据；
- 创建一个`sync.Cond`对象，关联这个互斥锁；
- 在需要等待条件变量的地方，获取这个互斥锁，并使用`Wait`方法等待条件变量被通知；
- 在需要通知等待的协程时，使用`Signal`或`Broadcast`方法通知等待的协程。
- 最后，释放这个互斥锁。

下面是一个简单的代码的示例，展示了大概的代码结构:
```go
var (
    // 1. 定义一个互斥锁
    mu    sync.Mutex
    cond  *sync.Cond
    count int
)
func init() {
    // 2.将互斥锁和sync.Cond进行关联
    cond = sync.NewCond(&mu)
}
go func(){
    // 3. 在需要等待的地方,获取互斥锁，调用Wait方法等待被通知
    mu.Lock()
    // 这里会不断循环判断 是否满足条件
    for !condition() {
       cond.Wait() // 等待任务
    }
    mu.Unlock()
}

go func(){
     // 执行业务逻辑
     // 4. 满足条件，此时调用Broadcast唤醒处于等待状态的协程
     cond.Broadcast() 
}
```



### 2.4 使用例子    
下面通过描述`net/http`中的 `connReader`，来展示使用`sync.Cond`实现阻塞等待通知的机制。这里我们只需要知道`connReader`存在下面两个方法:
```go
func (cr *connReader) Read(p []byte) (n int, err error) {}
func (cr *connReader) abortPendingRead() {}
```
`Read`方法则是用于从`HTTP`连接中读取数据，不允许并发访问的。而`abortPendingRead`则是用于终止`Read`方法继续读取数据。

从`abortPendingRead`方法的语意来看，是需要成功终止其他协程进行数据的读取之后，才能正常返回，也就是此时没有协程再继续读取数据了，才可以返回。

`那abortPendingRead`如何得知是否还有协程在读取数据呢，其实是可以通过定时轮训`connReader`的状态，从而判断当前Read方法是否仍在读取数据。但是定时轮训效率太低，可能会造成cpu的大量空转。**更好的方式，应该是让协程进入阻塞状态，然后等条件满足了，其他协程再来唤醒当前协程，然后再继续运行下去**。

这个其实就是`sync.Cond`设计的用途，当不满足运行条件时，先进入阻塞状态，等待条件满足时，再由其他协程来唤醒，然后再继续运行下去，能够提高程序的执行效率。其中`Wait`方法便是让协程进入阻塞状态，而`Singal`和`Boardcast`便是唤醒处于阻塞状态的协程，告知其条件满足了，可以继续向下执行了。

回到我们`connReader`的例子，我们使用`sync.Cond`实现阻塞等待通知的效果。
```
type connReader struct {
    // 是否正在读取数据
    inRead bool
    mu      sync.Mutex // guards following
    cond    *sync.Cond
}

func (cr *connReader) abortPendingRead() {
    if !cr.inRead{
        return
    }
    //1. 通过一定手段,让Read方法中断
    cr.mu.Lock()
    // 判断Read方法是否仍然在读取数据
    for cr.inRead {
        //2. 此时Read方法仍然在读取数据, 不满足条件，等待通知
        cr.cond.Wait()
    }
    cr.mu.Unlock()
}

func (cr *connReader) Read(p []byte) (n int, err error) {
     cr.mu.Lock()
     cr.inRead = true
    // 1. 读取数据
    // 2. abortPendingRead通过某种手段,让Read方法中断
    
    cr.inRead = false
    cr.mu.Unlock()
    // 3. 现在已经满足abortPendingRead继续执行下去的条件了，可以唤醒abortPendingRead协程了
    cond.Boardcast()
}
```
这里`abortPendingRead`方法首先判断是否还在读取数据，是的话，调用`Wait`方法进入阻塞状态，等待条件满足后继续执行。

对于`Read`方法，因为其不运行并发访问，当其将退出时，说明此时已经没有协程在读取数据了，满足`abortPendingRead`继续执行下去的条件了，此时可以调用`Boardcast`来唤醒等待条件满足的协程。之后调用`abortPendingRead`方法的协程此时能够接收到通知，便能够顺利被唤醒，从而正确返回。

这里便展示了一个简单的，使用`sync.Cond`实现阻塞等待通知的例子。


# 3. 原理
### 3.1 基本原理
在`Sync.Cond`存在一个通知队列，保存了所有处于等待状态的协程。通知队列定义如下:
```
type notifyList struct {
   wait   uint32
   notify uint32
   lock   uintptr // key field of the mutex
   head   unsafe.Pointer
   tail   unsafe.Pointer
}
```
当调用`Wait`方法时，此时`Wait`方法会释放所持有的锁，然后将自己放到`notifyList`等待队列中等待。此时会将当前协程加入到等待队列的尾部，然后进入阻塞状态。

当调用`Signal` 时，此时会唤醒等待队列中的第一个协程，其他继续等待。如果此时没有处于等待状态的协程，调用`Signal`不会有其他作用，直接返回。当调用`BoradCast`方法时，则会唤醒`notfiyList`中所有处于等待状态的协程。

`sync.Cond`的代码实现比较简单，协程的唤醒和阻塞已经由运行时包实现了，`sync.Cond`的实现直接调用了运行时包提供的API。

### 3.2 实现
####  3.2.1 Wait方法实现
`Wait`方法首先调用`runtime_notifyListAd`方法，将自己加入到等待队列中，然后释放锁，等待其他协程的唤醒。
```go
func (c *Cond) Wait() {
   // 将自己放到等待队列中
   t := runtime_notifyListAdd(&c.notify)
   // 释放锁
   c.L.Unlock()
   // 等待唤醒
   runtime_notifyListWait(&c.notify, t)
   // 重新获取锁
   c.L.Lock()
}
```

#### 3.2.2 Singal方法实现
`Singal`方法调用`runtime_notifyListNotifyOne`唤醒等待队列中的一个协程。
```go
func (c *Cond) Signal() {
   // 唤醒等待队列中的一个协程
   runtime_notifyListNotifyOne(&c.notify)
}
```

#### 3.2.3 Broadcast方法实现
`Broadcast`方法调用`runtime_notifyListNotifyAll`唤醒所有处于等待状态的协程。
```go
func (c *Cond) Broadcast() {
   // 唤醒等待队列中所有的协程
   runtime_notifyListNotifyAll(&c.notify)
}
```

# 4.使用注意事项
### 4.1 调用Wait方法前未加锁
#### 4.1.1 问题
如果在调用`Wait`方法前未加锁，此时会直接`panic`，下面是一个简单例子的说明:
```go
package main

import (
    "fmt"
    "sync"
    "time"
)

var (
   count int
   cond  *sync.Cond
   lk    sync.Mutex
)

func main() {
    cond = sync.NewCond(&lk)
    wg := sync.WaitGroup{}
    wg.Add(2)
    go func() {
       defer wg.Done()
       for {
          time.Sleep(time.Second)
          count++
          cond.Broadcast()
       }
    }()
    
    go func() {
       defer wg.Done()
       for {
          time.Sleep(time.Millisecond * 500)          
          //cond.L.Lock() 
          for count%10 != 0 {
               cond.Wait()
          }
          t.Logf("count = %d", count)
          //cond.L.Unlock()  
       }
    }()
    wg.Wait()
}
```
上面代码中，协程一每隔1s，将count字段的值自增1，然后唤醒所有处于等待状态的协程。协程二执行的条件为count的值为10的倍数，此时满足执行条件，唤醒后将会继续往下执行。

但是这里在调用`sync.Wait`方法前，没有先获取锁，下面是其执行结果，会抛出 fatal error: sync: unlock of unlocked mutex 错误，结果如下:
```
count = 0
fatal error: sync: unlock of unlocked mutex
```
因此，在调用`Wait`方法前，需要先获取到与`sync.Cond`关联的锁，否则会直接抛出异常。

#### 4.1.2 为什么调用Wait方法前需要先获取该锁
强制调用Wait方法前需要先获取该锁。这里的原因在于调用`Wait`方法如果不加锁，有可能会出现竞态条件。

这里假设多个协程都处于等待状态，然后一个协程调用了Broadcast唤醒了其中一个或多个协程，此时这些协程都会被唤醒。

如下，假设调用`Wait`方法前没有加锁的话，那么所有协程都会去调用`condition`方法去判断是否满足条件，然后都通过验证，执行后续操作。
```go
for !condition() {
    c.Wait()
}
c.L.Lock()
// 满足条件情况下,执行的逻辑
c.L.Unlock()
```
此时会出现的情况为，本来是需要在满足`condition`方法的前提下，才能执行的操作。现在有可能的效果，为前面一部分协程执行时，还是满足`condition`条件的；但是后面的协程，尽管不满足`condition`条件，还是执行了后续操作，可能导致程序出错。

正常的用法应该是，在调用`Wait`方法前便加锁，只会有一个协程判断是否满足`condition`条件，然后执行后续操作。这样子就不会出现即使不满足条件，也会执行后续操作的情况出现。
```go
c.L.Lock()
for !condition() {
    c.Wait()
}
// 满足条件情况下,执行的逻辑
c.L.Unlock()
```

### 4.2 Wait方法接收到通知后，未重新检查条件变量
调用`sync.Wait`方法，协程进入阻塞状态后被唤醒，没有重新检查条件变量，此时有可能仍然处于不满足条件变量的场景下。然后直接执行后续操作，有可能会导致程序出错。下面举一个简单的例子:
```go
package main

import (
    "fmt"
    "sync"
    "time"
)

var (
   count int
   cond  *sync.Cond
   lk    sync.Mutex
)

func main() {
    cond = sync.NewCond(&lk)
    wg := sync.WaitGroup{}
    wg.Add(3)
    go func() {
       defer wg.Done()
       for {
          time.Sleep(time.Second)
          cond.L.Lock()
          // 将flag 设置为true
          flag = true
          // 唤醒所有处于等待状态的协程
          cond.Broadcast()
          cond.L.Unlock()
       }
    }()
    
    for i := 0; i < 2; i++ {
       go func(i int) {
          defer wg.Done()
          for {
             time.Sleep(time.Millisecond * 500)
             cond.L.Lock()
             // 不满足条件，此时进入等待状态
             if !flag {
                cond.Wait()
             }
             // 被唤醒后，此时可能仍然不满足条件
             fmt.Printf("协程 %d flag = %t", i, flag)
             flag = false
             cond.L.Unlock()
          }
       }(i)
    }
    wg.Wait()
}
```
在这个例子，我们启动了一个协程，定时将`flag`设置为true，相当于每隔一段时间，便满足执行条件，然后唤醒所有处于等待状态的协程。

然后又启动了两个协程，在满足条件的前提下，开始执行后续操作，但是这里协程被唤醒后，没有重新检查条件变量，具体看第39行。这里会出现的场景是，第一个协程被唤醒后，此时执行后续操作，然后将`flag`重新设置为false，此时已经不满足条件了。之后第二个协程唤醒后，获取到锁，没有重新检查此时是否满足执行条件，直接向下执行，这个就和我们预期不符，可能会导致程序出错，代码执行效果如下:
```go
协程 1 flag = true
协程 0 flag = false
协程 1 flag = true
协程 0 flag = false
```
可以看到，此时协程0执行时，`flag`的值均为`false`,说明此时其实并不符合执行条件，可能会导致程序出错。因此正确用法应该像下面这样子，被唤醒后，需要重新检查条件变量，满足条件之后才能继续向下执行。
```go
c.L.Lock()
// 唤醒后,重新检查条件变量是否满足条件
for !condition() {
    c.Wait()
}
// 满足条件情况下,执行的逻辑
c.L.Unlock()
```

### 4.3 不能复制sync.Cond

这是因为`sync.Cond`类型包含了一个互斥锁(mutex)和一个`notifyList`，如果对其进行复制，此时已经处于使用状态的`sync.Mutex`会在另一个地方在不知情的情况下使用，这会导致不可预料的情况出现。

其次是`notifyList`也会被拷贝，`notifyList`保存了等待通知的goroutine列表。如果拷贝了`sync.Cond`类型的值，此时新的值和原始值都将指向 同一个等待通知的goroutine列表。对新的值调用`Singal`和`Broadcast`方法将会影响到原始`sync.Cond`中的等待通知的goroutine,这样子可能会导致重复唤醒问题的出现。

为了避免这种问题，我们通常将`sync.Cond`类型的值作为指针类型来使用，并使用`&`操作符来取得它的地址，这样就可以在不同的goroutine之间传递指向同一个`sync.Cond`值的指针，并避免对它进行拷贝。

同时，`sync.Cond`从实现上就禁止了`sync.Cond`的复制，在编译期就对其进行验证，一旦试图复制时，编译器会直接报错。

# 5.总结
本文介绍了 Go 语言中的 sync.Cond 并发原语，它是用于实现 goroutine 之间的同步的重要工具。我们首先学习了 `sync.Cond` 的基本使用方法，包括创建和使用条件变量、使用`Wait`和`Signal`/`Broadcast`方法等。

在接下来的部分中，我们介绍了 `sync.Cond` 的实现原理，主要是对等待队列的使用，从而`sync.Cond`有更好的理解，能够更好得使用它。同时，我们也讲述了使用`sync.Cond`的注意事项，如调用`Wait`方法前需要加锁等。

基于以上内容，本文完成了对 `sync.Cond` 的介绍，希望能够帮助大家更好地理解和使用Go语言中的并发原语。