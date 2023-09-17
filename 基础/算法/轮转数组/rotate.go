package 轮转数组


// 拷贝数组方式
func rotate(nums []int, k int) {
	if k == 0 || k%len(nums) == 0 {
		return
	}
	newNums := make([]int, len(nums))
	for i := 0; i < len(nums); i++ {
		newNums[(i+k) % len(nums)] = nums[i]
	}
	for i := 0; i < len(nums); i++ {
		nums[i] = newNums[i]
	}
}

// 反转数组方式
func reverse(nums []int, k int) {
	if k == 0 || k%len(nums) == 0 {
		return
	}
	k = k % len(nums)
	reverseSlice(nums)
	// 移动左边
	reverseSlice(nums[:k])
	reverseSlice(nums[k:])
}

func reverseSlice(nums []int) {
	l,r := 0, len(nums) - 1
	for l < r {
		tmp := nums[l]
		nums[l] = nums[r]
		nums[r] = tmp
		l ++
		r --
	}
}

// 环形替换
func ringReplace(nums []int, k int) {
	n := len(nums)
	k %= n
	// 循环次数
	time := gcd(k, n)
	// 需要进行多少轮循环
	for i := 0; i < time; i++ {
		// 开始一轮循环,指定当前元素下标 和 下一组元素下标
		start, cur := i, (i + k) % n
		// 前一个元素的值
		preVal := nums[start]
		// 当前将被替换元素的值
		curVal := nums[cur]
		// 没有回到原点,一直运行下去
		for cur != start{
			// 将元素向后移动
			nums[cur] = preVal
			// 将当前下标对应元素的值保留下来,后续会被移动到后面
			preVal = curVal
			// 获取下一个位置
			cur = (cur + k) % n
			curVal = nums[cur]
		}
		// 回到原点后,将最后一个元素进行赋值
		nums[cur] = preVal
	}

}


func gcd(a, b int) int {
	for a != 0 {
		a, b = b%a, a
	}
	return b
}