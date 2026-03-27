package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// SpinnerType 定义 spinner 类型
type SpinnerType int

const (
	SpinnerDots SpinnerType = iota
	SpinnerLine
	SpinnerCircle
	SpinnerArrow
)

// spinnerFrames 定义各种 spinner 的帧
var spinnerFrames = map[SpinnerType][]string{
	SpinnerDots:   {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	SpinnerLine:   {"|", "/", "-", "\\"},
	SpinnerCircle: {"◜", "◠", "◝", "◞", "◡", "◟"},
	SpinnerArrow:  {"←", "↖", "↑", "↗", "→", "↘", "↓", "↙"},
}

// Spinner 进度动画
type Spinner struct {
	frames   []string
	current  int
	message  string
	started  bool
	stopChan chan struct{}
	mu       sync.Mutex
}

// NewSpinner 创建新的 spinner
func NewSpinner(spinnerType SpinnerType, message string) *Spinner {
	frames, ok := spinnerFrames[spinnerType]
	if !ok {
		frames = spinnerFrames[SpinnerDots]
	}
	return &Spinner{
		frames:   frames,
		message:  message,
		stopChan: make(chan struct{}),
	}
}

// Start 开始 spinner 动画
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	go func() {
		for {
			select {
			case <-s.stopChan:
				return
			default:
				s.render()
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

// render 渲染一帧
func (s *Spinner) render() {
	s.mu.Lock()
	frame := s.frames[s.current]
	s.current = (s.current + 1) % len(s.frames)
	msg := s.message
	s.mu.Unlock()

	// 清除行并打印新内容
	ClearLine()
	if colorEnabled {
		fmt.Printf("\r%s %s", Info.Sprint(frame), msg)
	} else {
		fmt.Printf("\r[%s] %s", frame, msg)
	}
}

// Update 更新消息
func (s *Spinner) Update(message string) {
	s.mu.Lock()
	s.message = message
	s.mu.Unlock()
}

// Stop 停止 spinner
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	s.started = false
	s.mu.Unlock()

	s.stopChan <- struct{}{}
	ClearLine()
}

// StopWithMessage 停止 spinner 并显示最终消息
func (s *Spinner) StopWithMessage(icon string, message string) {
	s.Stop()
	if colorEnabled {
		fmt.Printf("\r%s %s\n", Success.Sprint(icon), message)
	} else {
		fmt.Printf("\r[%s] %s\n", icon, message)
	}
}

// StopWithSuccess 停止并显示成功消息
func (s *Spinner) StopWithSuccess(message string) {
	s.StopWithMessage(IconSuccess, message)
}

// StopWithError 停止并显示错误消息
func (s *Spinner) StopWithError(message string) {
	s.Stop()
	if colorEnabled {
		fmt.Printf("\r%s %s\n", Error.Sprint(IconError), message)
	} else {
		fmt.Printf("\r[%s] %s\n", "ERROR", message)
	}
}

// ProgressBar 进度条
type ProgressBar struct {
	total     int
	current   int
	width     int
	desc      string
	startTime time.Time
	mu        sync.Mutex
}

// NewProgressBar 创建新的进度条
func NewProgressBar(total int, desc string) *ProgressBar {
	return &ProgressBar{
		total:     total,
		current:   0,
		width:     40,
		desc:      desc,
		startTime: time.Now(),
	}
}

// Add 增加进度
func (p *ProgressBar) Add(n int) {
	p.mu.Lock()
	p.current += n
	if p.current > p.total {
		p.current = p.total
	}
	p.render()
	p.mu.Unlock()
}

// Set 设置当前进度
func (p *ProgressBar) Set(current int) {
	p.mu.Lock()
	p.current = current
	if p.current > p.total {
		p.current = p.total
	}
	p.render()
	p.mu.Unlock()
}

// render 渲染进度条
func (p *ProgressBar) render() {
	percent := float64(p.current) / float64(p.total)
	filled := int(percent * float64(p.width))
	empty := p.width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	elapsed := time.Since(p.startTime).Seconds()

	// 清除行并打印进度条
	ClearLine()
	if colorEnabled {
		fmt.Printf("\r%s [%s] %d/%d (%.0f%%) %.1fs",
			Dim.Sprint(p.desc),
			Success.Sprint(bar),
			p.current,
			p.total,
			percent*100,
			elapsed,
		)
	} else {
		fmt.Printf("\r%s [%s] %d/%d (%.0f%%) %.1fs",
			p.desc,
			bar,
			p.current,
			p.total,
			percent*100,
			elapsed,
		)
	}
}

// Done 完成进度条
func (p *ProgressBar) Done() {
	p.Set(p.total)
	fmt.Println()
}

// MultiProgress 多任务进度
type MultiProgress struct {
	tasks    map[string]*TaskProgress
	mu       sync.Mutex
	maxTitle int
}

// TaskProgress 单个任务进度
type TaskProgress struct {
	title    string
	status   TaskStatus
	message  string
	spinner  int
}

// TaskStatus 任务状态
type TaskStatus int

const (
	TaskRunning TaskStatus = iota
	TaskDone
	TaskError
)

// NewMultiProgress 创建多任务进度
func NewMultiProgress() *MultiProgress {
	return &MultiProgress{
		tasks: make(map[string]*TaskProgress),
	}
}

// AddTask 添加任务
func (p *MultiProgress) AddTask(id, title string) {
	p.mu.Lock()
	if len(title) > p.maxTitle {
		p.maxTitle = len(title)
	}
	p.tasks[id] = &TaskProgress{
		title:   title,
		status:  TaskRunning,
		spinner: 0,
	}
	p.render()
	p.mu.Unlock()
}

// UpdateTask 更新任务
func (p *MultiProgress) UpdateTask(id string, status TaskStatus, message string) {
	p.mu.Lock()
	if task, ok := p.tasks[id]; ok {
		task.status = status
		task.message = message
	}
	p.render()
	p.mu.Unlock()
}

// CompleteTask 完成任务
func (p *MultiProgress) CompleteTask(id string) {
	p.UpdateTask(id, TaskDone, "")
}

// ErrorTask 任务出错
func (p *MultiProgress) ErrorTask(id string, message string) {
	p.UpdateTask(id, TaskError, message)
}

// render 渲染所有任务
func (p *MultiProgress) render() {
	// 先清除现有行数
	if len(p.tasks) > 0 {
		MoveUp(len(p.tasks))
	}

	// 打印所有任务
	for id, task := range p.tasks {
		ClearLine()
		switch task.status {
		case TaskRunning:
			frames := spinnerFrames[SpinnerDots]
			frame := frames[task.spinner%len(frames)]
			task.spinner++
			if colorEnabled {
				fmt.Printf("%s %-*s %s\n", Info.Sprint(frame), p.maxTitle, task.title, Dim.Sprint(task.message))
			} else {
				fmt.Printf("[%s] %-*s %s\n", frame, p.maxTitle, task.title, task.message)
			}
		case TaskDone:
			if colorEnabled {
				fmt.Printf("%s %-*s\n", Success.Sprint(IconSuccess), p.maxTitle, task.title)
			} else {
				fmt.Printf("[OK] %-*s\n", p.maxTitle, task.title)
			}
		case TaskError:
			if colorEnabled {
				fmt.Printf("%s %-*s %s\n", Error.Sprint(IconError), p.maxTitle, task.title, task.message)
			} else {
				fmt.Printf("[ERR] %-*s %s\n", p.maxTitle, task.title, task.message)
			}
		}
		_ = id // 避免未使用变量警告
	}
}