package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
	"math/rand"
	"github.com/eiannone/keyboard"
)

const (
	width  = 20
	height = 10
)

type Point struct {
	x, y int
}

type Snake struct {
	body  []Point
	dir   Point
	grow  bool
}

func (s *Snake) Move() {
	head := s.body[0]
	newHead := Point{head.x + s.dir.x, head.y + s.dir.y}

	// Insert new head
	s.body = append([]Point{newHead}, s.body...)

	// Remove tail if not growing
	if !s.grow {
		s.body = s.body[:len(s.body)-1]
	} else {
		s.grow = false
	}
}

func (s *Snake) ChangeDir(newDir Point) {
	// Prevent the snake from reversing
	if s.dir.x+newDir.x != 0 || s.dir.y+newDir.y != 0 {
		s.dir = newDir
	}
}

func (s *Snake) Eats(food Point) bool {
	return s.body[0] == food
}

func (s *Snake) Collision() bool {
	head := s.body[0]
	if head.x < 0 || head.y < 0 || head.x >= width || head.y >= height {
		return true
	}
	for _, b := range s.body[1:] {
		if b == head {
			return true
		}
	}
	return false
}

func randomFood(snake Snake) Point {
	var food Point
	for {
		food = Point{rand.Intn(width), rand.Intn(height)}
		collides := false
		for _, b := range snake.body {
			if b == food {
				collides = true
				break
			}
		}
		if !collides {
			break
		}
	}
	return food
}

func clearScreen() {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func printState(snake Snake, food Point) {
	board := make([][]rune, height)
	for i := range board {
		board[i] = make([]rune, width)
		for j := range board[i] {
			board[i][j] = ' '
		}
	}
	for _, b := range snake.body {
		board[b.y][b.x] = '#'
	}
	board[food.y][food.x] = '*'

	clearScreen()
	for _, line := range board {
		fmt.Println(string(line))
	}
	fmt.Println("Use arrow keys to move. Press ESC to quit.")
}

func main() {
	rand.Seed(time.Now().UnixNano())

	snake := Snake{
		body: []Point{{width / 2, height / 2}},
		dir:  Point{0, 1},
	}

	food := randomFood(snake)

	if err := keyboard.Open(); err != nil {
		panic(err)
	}
	defer keyboard.Close()

	for {
		// Print current game state
		printState(snake, food)

		// Handle keyboard input
		if char, key, err := keyboard.GetKey(); err == nil {
			switch key {
			case keyboard.KeyArrowUp:
				snake.ChangeDir(Point{0, -1})
			case keyboard.KeyArrowDown:
				snake.ChangeDir(Point{0, 1})
			case keyboard.KeyArrowLeft:
				snake.ChangeDir(Point{-1, 0})
			case keyboard.KeyArrowRight:
				snake.ChangeDir(Point{1, 0})
			case keyboard.KeyEsc:
				return
			}
		}

		// Move snake
		snake.Move()

		// Check for collision
		if snake.Collision() {
			fmt.Println("Game Over!")
			return
		}

		// Check for eating food
		if snake.Eats(food) {
			snake.grow = true
			food = randomFood(snake)
		}

		// Slow down game loop
		time.Sleep(200 * time.Millisecond)
	}
}
