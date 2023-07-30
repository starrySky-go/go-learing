# 1. 简介
本文主要介绍 Go 语言中的 Once 并发原语，包括 Once 的基本使用方法、原理和注意事项，从而对 Once 的使用有基本的了解。

# 2. 基本使用
### 2.1 基本定义
`sync.Once`是Go语言中的一个并发原语，用于保证某个函数只被执行一次。`Once`类型有一个`Do`方法，该方法接收一个函数作为参数，并在第一次调用时执行该函数。如果`Do`方法被多次调用，只有第一次调用会执行传入的函数。

### 2.2 使用方式
使用`sync.Once`非常简单，只需要创建一个`Once`类型的变量，然后在需要保证函数只被执行一次的地方调用其`Do`方法即可。下面是一个简单的例子：
```go
var once sync.Once

func initOperation() {
    // 这里执行一些初始化操作，只会被执行一次
}
func main() {
    // 在程序启动时执行initOperation函数，保证初始化只被执行一次
    once.Do(initOperation)   
    // 后续代码
}
```

### 2.3 使用例子
下面是一个简单使用`sync.Once`的例子，其中我们使用`sync.Once`来保证全局变量config只会被初始化一次：
````go
package main

import (
    "fmt"
    "sync"
)
var (
    config map[string]string
    once   sync.Once
)

func loadConfig() {
    // 模拟从配置文件中加载配置信息
    fmt.Println("load config...")
    config = make(map[string]string)
    config["host"] = "127.0.0.1"
    config["port"] = "8080"
}

func GetConfig() map[string]string {
    once.Do(loadConfig)
    return config
}

func main() {
    // 第一次调用GetConfig会执行loadConfig函数，初始化config变量
    fmt.Println(GetConfig())
    // 第二次调用GetConfig不会执行loadConfig函数，直接返回已初始化的config变量
    fmt.Println(GetConfig())
}
````

在这个例子中，我们定义了一个全局变量`config`和一个`sync.Once`类型的变量`once`。在`GetConfig`函数中，我们通过调用`once.Do`方法来保证`loadConfig`函数只会被执行一次，从而保证`config`变量只会被初始化一次。
运行上面的程序，输出如下:
````go
load config...
map[host:127.0.0.1 port:8080]
map[host:127.0.0.1 port:8080]
````

可以看到，`GetConfig`函数在第一次调用时执行了`loadConfig`函数，初始化了`config`变量。在第二次调用时，`loadConfig`函数不会被执行，直接返回已经初始化的`config`变量。


# 3. 原理

下面是`sync.Once`的具体实现如下：
````go
type Once struct {
   done uint32
   m    Mutex
}

func (o *Once) Do(f func()) {    
    // 判断done标记位是否为0
   if atomic.LoadUint32(&o.done) == 0 {
      // Outlined slow-path to allow inlining of the fast-path.
      o.doSlow(f)
   }
}

func (o *Once) doSlow(f func()) {
   // 加锁
   o.m.Lock()
   defer o.m.Unlock()
   // 执行双重检查,再次判断函数是否已经执行
   if o.done == 0 {
      defer atomic.StoreUint32(&o.done, 1)
      f()
   }
}
````

`sync.Once`的实现原理比较简单，主要依赖于一个`done`标志位和一个互斥锁。当`Do`方法被第一次调用时，会先原子地读取`done`标志位，如果该标志位为0，说明函数还没有被执行过，此时会加锁并执行传入的函数，并将`done`标志位置为1，然后释放锁。如果标志位为1，说明函数已经被执行过了，直接返回。


# 4. 使用注意事项
### 4.1 不能将sync.Once作为函数局部变量
下面是一个简单的例子，说明将 `sync.Once` 作为局部变量会导致的问题：
````go
var config map[string]string
func initConfig() {
    fmt.Println("initConfig called")
    config["1"] = "hello world"
}

func getConfig() map[string]string{
    var once sync.Once
    once.Do(initCount)
    fmt.Println("getConfig called")
    
}

func main() {
    for i := 0; i < 10; i++ {
        go getConfig()
    }
    time.Sleep(time.Second)
}
````
这里初始化函数会被多次调用，这与`initConfig` 方法只会执行一次的预期不符。这是因为将 `sync.Once` 作为局部变量时，每次调用函数都会创建新的 `sync.Once` 实例，每个 `sync.Once` 实例都有自己的 `done` 标志，多个实例之间无法共享状态。导致初始化函数会被多次调用。

如果将 `sync.Once` 作为全局变量或包级别变量，就可以避免这个问题。所以基于此，不能定义`sync.Once` 作为函数局部变量来使用。

### 4.2 不能在`once.Do`中再次调用`once.Do`
下面举一个在`once.Do`方法中再次调用`once.Do` 方法的例子:
````go
package main

import (
"fmt"
"sync"
)

func main() {
   var once sync.Once
   var onceBody func()
   
   onceBody = func() {
      fmt.Println("Only once")
      once.Do(onceBody) // 再次调用once.Do方法
   }

   // 执行once.Do方法
   once.Do(onceBody)

   fmt.Println("done")
}
````
在上述代码中，当`once.Do(onceBody)`第一次执行时，会输出"Only once"，然后在执行`once.Do(onceBody)`时会发生死锁，程序无法继续执行下去。

这是因为`once.Do()`方法在执行过程中会获取互斥锁，在方法内再次调用`once.Do()`方法，那么就会在获取互斥锁时出现死锁。

因此，我们不能在once.Do方法中再次调用once.Do方法。


### 4.3 需要对传入的函数进行错误处理
#### 4.3.1 基本说明
一般情况下，如果传入的函数不会出现错误，可以不进行错误处理。但是，如果传入的函数可能出现错误，就必须对其进行错误处理，否则可能会导致程序崩溃或出现不可预料的错误。

因此，在编写传入Once的Do方法的函数时，需要考虑到错误处理问题，保证程序的健壮性和稳定性。

#### 4.3.2 未错误处理导致的问题
下面举一个传入的函数可能出现错误，但是没有对其进行错误处理的例子:
````go
import (
   "fmt"
   "net"
   "sync"
)

var (
   initialized bool
   connection  net.Conn
   initOnce    sync.Once
)

func initConnection() {
   connection, _ = net.Dial("tcp", "err_address")
}

func getConnection() net.Conn {
   initOnce.Do(initConnection)
   return connection
}

func main() {
   conn := getConnection()
   fmt.Println(conn)
   conn.Close()
}
````

在上面例子中，其中`initConnection` 为传入的函数，用于建立TCP网络连接，但是在`sync.Once`中执行该函数时，是有可能返回错误的，而这里并没有进行错误处理，直接忽略掉错误。此时调用`getConnection` 方法,如果`initConnection`报错的话，获取连接时会返回空连接，后续调用将会出现空指针异常。因此，如果传入`sync.Once`当中的函数可能发生异常，此时应该需要对其进行处理。

#### 4.3.3 处理方式
##### 4.3.3.1 panic退出执行
应用程序第一次启动时，此时调用`sync.Once`来初始化一些资源，此时发生错误，同时初始化的资源是必须初始化的，可以考虑在出现错误的情况下，使用panic将程序退出，避免程序继续执行导致更大的问题。具体代码示例如下:
````go
import (
   "fmt"
   "net"
   "sync"
)

var (
   connection  net.Conn
   initOnce    sync.Once
)

func initConnection() {
   // 尝试建立连接
   connection, err = net.Dial("tcp", "err_address")
    if err != nil {
       panic("net.Dial error")
    }
}

func getConnection() net.Conn {
   initOnce.Do(initConnection)
   return connection
}
````
如上，当initConnection方法报错后，此时我们直接panic,退出整个程序的执行。

##### 4.3.3.2 修改`sync.Once`实现，Do函数的语意修改为只成功执行一次
在程序运行过程中，可以选择记录下日志或者返回错误码，而不需要中断程序的执行。然后下次调用时再执行初始化的逻辑。这里需要对`sync.Once`进行改造，原本`sync.Once`中Do函数的实现为执行一次，这里将其修改为只成功执行一次。具体使用方式需要根据具体业务场景来决定。下面是其中一个实现:
````go
type MyOnce struct {
   done int32
   m    sync.Mutex
}

func (o *MyOnce) Do(f func() error) {
   if atomic.LoadInt32(&o.done) == 0 {
      o.doSlow(f)
   }
}

func (o *MyOnce) doSlow(f func() error) {
   o.m.Lock()
   defer o.m.Unlock()
   if o.done == 0 {
      // 只有在函数调用不返回err时,才会设置done
      if err := f(); err == nil {
         atomic.StoreInt32(&o.done, 1)
      }
   }
}
````
上述代码中，增加了一个错误处理逻辑。当 `f()` 函数返回错误时，不会将 `done` 标记位置为 1，以便下次调用时可以重新执行初始化逻辑。

需要注意的是，这种方式虽然可以解决初始化失败后的问题，但可能会导致初始化函数被多次调用。因此，在编写`f()` 函数时，需要考虑到这个问题，以避免出现不可预期的结果。

下面是一个简单的例子，使用我们重新实现的Once，展示第一次初始化失败时，第二次调用会重新执行初始化逻辑，并成功初始化：
```go
var (
   hasCall bool
   conn    net.Conn
   m       MyOnce
)

func initConn() (net.Conn, error) {
   fmt.Println("initConn...")
   // 第一次执行,直接返回错误
   if !hasCall {
      return nil, errors.New("init error")
   }
   // 第二次执行,初始化成功,这里默认其成功
   conn, _ = net.Dial("tcp", "baidu.com:80")
   return conn, nil
}

func GetConn() (net.Conn, error) {
   m.Do(func() error {
      var err error
      conn, err = initConn()
      if err != nil {
         return err
      }
      return nil
   })
   // 第一次执行之后,将hasCall设置为true,让其执行初始化逻辑
   hasCall = true
   return conn, nil
}
 
func main() {
   // 第一次执行初始化逻辑,失败
   GetConn()
   // 第二次执行初始化逻辑,还是会执行,此次执行成功
   GetConn()
   // 第二次执行成功,第三次调用,将不会执行初始化逻辑
   GetConn()
}
```
在这个例子中，第一次调用`Do`方法初始化失败了，`done`标记位被设置为0。在第二次调用`Do`方法时，由于`done`标记位为0，会重新执行初始化逻辑，这次初始化成功了，`done`标记位被设置为1。第三次调用，由于之前`Do`方法已经执行成功了，不会再执行初始化逻辑。

# 5. 总结
本文旨在介绍Go语言中的Once并发原语，包括其基本使用、原理和注意事项，让大家对Once有一个基本的了解。

首先，我们通过示例演示了Once的基本使用方法，并强调了其仅会执行一次的特性。然后，我们解释了Once仅执行一次的原因，使读者更好地理解Once的工作原理。最后，我们指出了使用Once时的一些注意事项，以避免误用。

总之，本文全面地介绍了Go语言中的Once并发原语，使读者能够更好地理解和应用它。