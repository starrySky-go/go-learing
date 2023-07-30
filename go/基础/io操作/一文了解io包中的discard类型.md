# 1. 引言

`io.discard`是Go语言标准库提供一个结构体类型，其在丢弃不需要的数据场景下非常好用。本文我们将从`io.discard` 类型的基本定义出发，讲述其基本使用和实现原理，接着简单描述 `io.discard` 的使用场景，基于此完成对 `io.discard` 类型的介绍。

# 2. 介绍
### 2.1 基本定义

`io.discard` 是 Go语言提供的一个`Writer`，这个`Writer` 比较特殊，其不会做任何事情。它会将写入的数据**立即丢弃**，不会做任何处理。其定义如下:
```go
type discard struct{}
func (discard) Write(p []byte) (int, error) {}
func (discard) WriteString(s string) (int, error) {}
func (discard) ReadFrom(r Reader) (n int64, err error) {}
```
`discard` 结构体类型没有定义任何字段，同时还提供了`Write` ,`ReadFrom`和`WriteString` 方法，`Write` 方法和`WriteString` 方法分别接收字节切片和字符串，然后返回写入的字节数。

同时还实现了`io.ReaderFrom` 接口，这个是为了在使用 `io.Copy` 函数时，将数据从源复制到`io.discard` 时，避免不必要的操作。

从上面`discard` 的定义可以看起来，其不是一个公开类型的结构体类型，所以我们并不能创建结构体实例。事实上Go语言提供了一个`io.discard` 实例的预定义常量，我们直接使用，无需自己创建实例，定义如下:
```go
var Discard Writer = discard{}
```
###   2.2 使用说明
下面通过一个丢弃网络连接中不再需要的数据的例子，来展示`io.Discard` 的使用，代码示例如下:
```go
package main

import (
        "fmt"
        "io"
        "net"
        "os"
)

func discardData(conn net.Conn, bytesToDiscard int64) error {
        _, err := io.CopyN(io.Discard, conn, bytesToDiscard)
        return err
}

func main() {
        conn, err := net.Dial("tcp", "example.com:80")
        if err != nil {
                fmt.Println("连接错误:", err)
                return
        }
        defer conn.Close()

        bytesToDiscard := int64(1024) // 要丢弃的字节数

        err = discardData(conn, bytesToDiscard)
        if err != nil {
                fmt.Println("丢弃数据错误:", err)
                return
        }

        fmt.Println("数据已成功丢弃。")
}
```
在上面示例中，我们建立了网络连接，然后连接中的前1024个字节的数据是不需要的。这个时候，我们通过`io.CopyN` 函数将数据从`conn` 拷贝到`io.Discard` 当中，基于`io.Discard` 丢弃数据的特性，成功将连接的前1024个字节丢弃掉，而不需要自定义缓冲区之类的操作，简单高效。

# 3. 实现原理
`io.Discard`的目的是在某些场景下提供一个满足`io.Writer`接口的实例，但用户对于数据的写入操作并不关心。它可以被用作一个黑洞般的写入目标，默默地丢弃所有写入它的数据。所以`io.discard` 的实现也相对比较简单，不对输入的数据进行任何处理即可，下面我们来看具体的实现。

首先是`io.discard` 结构体的定义，没有定义任何字段，因为本来也不需要执行任何写入操作:
```go
type discard struct{}
```

而对于`Write` 和 `WriteString` 方法，其直接返回了传入参数的长度，往该`Writer` 写入的数据不会被写入到其他地方，而是被直接丢弃:
```go
func (discard) Write(p []byte) (int, error) {
   return len(p), nil
}

func (discard) WriteString(s string) (int, error) {
   return len(s), nil
}
```
同时`discard` 也实现了`io.ReaderFrom` 接口，实现了`ReadFrom` 方法，实现也是非常简单，从`blackHolePool` 缓冲池中获取字节切片，然后不断读取数据，读取完成之后，再将字节切片重新放入缓冲池当中:
```go
// 存在一个字节切片缓冲池
var blackHolePool = sync.Pool{
   New: func() any {
      b := make([]byte, 8192)
      return &b
   },
}

func (discard) ReadFrom(r Reader) (n int64, err error) {
   // 从缓冲池中取出一个 字节切片
   bufp := blackHolePool.Get().(*[]byte)
   readSize := 0
   for {
      // 不断读取数据，bufp 只是作为一个读取数据的中介，读取到的数据并无意义
      readSize, err = r.Read(*bufp)
      n += int64(readSize)
      if err != nil {
         // 将字节切片 重新放入到 blackHolePool 当中
         blackHolePool.Put(bufp)
         if err == EOF {
            return n, nil
         }
         return
      }
   }
}
```
在`io.Copy` 函数中，将调用`discard` 中的`ReadFrom` 方法，能够将`Writer`中的所有数据读取完，然后丢弃掉。

# 4. 使用场景
`io.Discard` 给我们提供了一个`io.Writer` 接口的实例，同时其又不会真实得写入数据，这个在某些场景下非常有用。

有时候，我们可能需要一个实现`io.Writer` 接口的实例，但是我们并不关心数据写入`Writer` 的结果，也不关心数据是否写到了哪个地方，此时`io.Discard` 就给我们提供了一个方便的解决方案。同时`io.Discard` 可以作为一个黑洞写入目标，能够将数据默默丢弃掉，不会进行实际的处理和存储。

所以如果我们想要丢弃某些数据，亦或者是需要一个`io.Writer`接口的实例，但是对于写入结果不需要关注时，此时使用`io.Discard` 是非常合适的。

# 5. 总结

`io.discard` 函数是Go语言标准库中一个实现了`Writer`接口的结构体类型，能够悄无声息得实现数据的丢弃。 我们先从`io.discard` 类型的基本定义出发，之后通过一个简单的示例，展示如何使用`io.discard` 类型实现对不需要数据的丢弃。

接着我们讲述了`io.discard` 类型的实现原理，其实就是不对写入的数据执行任何操作。在使用场景下，我们想要丢弃某些数据，亦或者是需要一个`io.Writer`接口的实例，但是对于写入结果不需要关注时，此时使用`io.Discard` 是非常合适的。

基于此，便完成了对`io.discard` 类型的介绍，希望对你有所帮助。