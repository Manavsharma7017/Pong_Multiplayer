package main

import (
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var rng = rand.New(rand.NewSource(rand.Int63()))

const MAX_PLAYERS = 2
const GAME_WIDTH = 500
const GAME_HEIGHT = 500
const PADDLE_WIDTH = 25
const PADDLE_HEIGHT = 100
const BALL_RADIUS = 12
const PADDLE_SPEED = 50

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	conn *websocket.Conn
	id   string
	role string // player1 or player2
}

type Playermessage struct {
	Type string `json:"type"`
	Role string `json:"role"`
	ID   string `json:"id"`
}
type Ball struct {
	X     int `json:"x"`
	Y     int `json:"y"`
	Speed int `json:"speed"`
	Dx    int `json:"dx"`
	Dy    int `json:"dy"`
}

type Paddlemovement struct {
	Y      int    `json:"y"`
	X      int    `json:"x"`
	Role   string `json:"role"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Id     string `json:"id"`
}

type Score struct {
	Player1 int `json:"player1"`
	Player2 int `json:"player2"`
}

type Gamestate struct {
	Type    string         `json:"type"`
	Ball    Ball           `json:"ball"`
	Paddle1 Paddlemovement `json:"paddle1"`
	Paddle2 Paddlemovement `json:"paddle2"`
	Score   Score          `json:"score"`
}
type MovementMessage struct {
	Type       string `json:"type"`
	Direction  string `json:"direction"`
	PlayerId   string `json:"playerId"`
	PlayerRole string `json:"playerRole"`
}

var (
	clients      = make(map[string]*Client)
	clientsMu    sync.Mutex // 🔒 ONLY ADDITION
	addClient    = make(chan *Client)
	removeClient = make(chan *Client)
	broadcast    = make(chan []byte)
)
var (
	game     *Gamestate
	gameMu   sync.Mutex
	stopGame chan struct{}
)

func main() {
	http.HandleFunc("/ws", handleWS)
	fs := http.FileServer(http.Dir("./dist"))
	http.Handle("/", fs)
	go manager()

	log.Println("WebSocket server running on ws://localhost:8080/ws")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	client := &Client{
		conn: conn,
		id:   conn.RemoteAddr().String(),
	}

	addClient <- client
	go handleMessages(client)
}

func handleMessages(c *Client) {
	defer func() {
		removeClient <- c
		c.conn.Close()
	}()

	for {
		var msg MovementMessage
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			log.Println("Read error:", err)
			break
		}
		if msg.Type == "MOVEMENT" {
			paddleMovement(msg.PlayerId, msg.PlayerRole, msg.Direction)
		}
	}
}
func paddleMovement(playerID, role, direction string) {
	gameMu.Lock()
	defer gameMu.Unlock()

	// validate game exists
	if game == nil {
		return
	}

	switch role {

	case "player1":
		if game.Paddle1.Id != playerID {
			return // 🚫 not your paddle
		}

		if direction == "up" {
			game.Paddle1.Y -= PADDLE_SPEED
		}
		if direction == "down" {
			game.Paddle1.Y += PADDLE_SPEED
		}

		// clamp
		if game.Paddle1.Y < 0 {
			game.Paddle1.Y = 0
		}
		if game.Paddle1.Y > GAME_HEIGHT-game.Paddle1.Height {
			game.Paddle1.Y = GAME_HEIGHT - game.Paddle1.Height
		}

	case "player2":
		if game.Paddle2.Id != playerID {
			return
		}

		if direction == "up" {
			game.Paddle2.Y -= PADDLE_SPEED
		}
		if direction == "down" {
			game.Paddle2.Y += PADDLE_SPEED
		}

		if game.Paddle2.Y < 0 {
			game.Paddle2.Y = 0
		}
		if game.Paddle2.Y > GAME_HEIGHT-game.Paddle2.Height {
			game.Paddle2.Y = GAME_HEIGHT - game.Paddle2.Height
		}
	}
}

func manager() {
	for {
		select {

		case client := <-addClient:
			clientsMu.Lock()
			if len(clients) >= MAX_PLAYERS {
				clientsMu.Unlock()
				client.conn.Close()
				continue
			}

			if len(clients) == 0 {
				client.role = "player1"
			} else if len(clients) == 1 {
				client.role = "player2"
			}

			clients[client.id] = client
			clientsMu.Unlock()

			broadcastToAll([]byte("new player joined: " + client.role))

			if len(clients) == 1 {
				broadcastToAll([]byte("waiting for opponent"))
			}

			if len(clients) == 2 {
				startGame()
			}

		case client := <-removeClient:
			clientsMu.Lock()
			delete(clients, client.id)
			clientsMu.Unlock()

			broadcastToAll([]byte("player disconnected"))

		case msg := <-broadcast:
			broadcastToAll(msg)
		}
	}
}

func broadcastToAll(msg []byte) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	for _, c := range clients {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			c.conn.Close()
			delete(clients, c.id)
		}
	}
}

func broadcastToclient(c *Client, mess *Playermessage) {
	if err := c.conn.WriteJSON(mess); err != nil {
		c.conn.Close()
		clientsMu.Lock()
		delete(clients, c.id)
		clientsMu.Unlock()
	}
}
func Ballcreation() Ball {
	return Ball{
		X:     GAME_WIDTH / 2,
		Y:     GAME_HEIGHT / 2,
		Speed: 1,
		Dx:    []int{-1, 1}[rng.Intn(2)],
		Dy:    []int{-1, 1}[rng.Intn(2)],
	}
}

func updateball(gameball *Ball) {
	gameball.X += gameball.Dx * gameball.Speed
	gameball.Y += gameball.Dy * gameball.Speed
}
func collisionwithwall(state *Gamestate) {
	// top wall
	if state.Ball.Y <= BALL_RADIUS {
		state.Ball.Y = BALL_RADIUS
		state.Ball.Dy *= -1
	}

	// bottom wall
	if state.Ball.Y >= GAME_HEIGHT-BALL_RADIUS {
		state.Ball.Y = GAME_HEIGHT - BALL_RADIUS
		state.Ball.Dy *= -1
	}

	// left goal
	if state.Ball.X <= BALL_RADIUS {
		state.Score.Player2++
		state.Ball = Ballcreation()
		return
	}

	// right goal
	if state.Ball.X >= GAME_WIDTH-BALL_RADIUS {
		state.Score.Player1++
		state.Ball = Ballcreation()
		return
	}
}

func collisionwithpaddle(state *Gamestate) {
	b := &state.Ball

	// player1 paddle
	if b.Dx < 0 &&
		b.X <= state.Paddle1.X+state.Paddle1.Width+BALL_RADIUS &&
		b.Y+BALL_RADIUS >= state.Paddle1.Y &&
		b.Y-BALL_RADIUS <= state.Paddle1.Y+state.Paddle1.Height {

		b.X = state.Paddle1.X + state.Paddle1.Width + BALL_RADIUS
		b.Dx = 1
		b.Speed++
	}

	// player2 paddle
	if b.Dx > 0 &&
		b.X >= state.Paddle2.X-BALL_RADIUS &&
		b.Y+BALL_RADIUS >= state.Paddle2.Y &&
		b.Y-BALL_RADIUS <= state.Paddle2.Y+state.Paddle2.Height {

		b.X = state.Paddle2.X - BALL_RADIUS
		b.Dx = -1
		b.Speed++
	}
}

func Mainlogic(state *Gamestate, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			log.Println("Game loop stopped")
			return

		case <-ticker.C:
			gameMu.Lock()
			updateball(&state.Ball)
			collisionwithwall(state)
			collisionwithpaddle(state)
			gameMu.Unlock()

			broadcastGame(state)
		}
	}
}

func broadcastGame(state *Gamestate) {
	clientsMu.Lock()
	snapshot := make([]*Client, 0, len(clients))
	for _, c := range clients {
		snapshot = append(snapshot, c)
	}
	clientsMu.Unlock()

	for _, c := range snapshot {
		if err := c.conn.WriteJSON(state); err != nil {
			c.conn.Close()
			clientsMu.Lock()
			delete(clients, c.id)
			clientsMu.Unlock()
		}
	}
}

func Restart_Game() {
	startGame()
}
func startGame() {
	// stop previous game safely
	if stopGame != nil {
		select {
		case <-stopGame:
			// already closed
		default:
			close(stopGame)
		}
	}

	stopGame = make(chan struct{})

	broadcastToAll([]byte("start game"))

	var paddle1, paddle2 Paddlemovement

	clientsMu.Lock()
	for _, c := range clients {
		broadcastToclient(c, &Playermessage{
			Type: "PLAYER_INFO",
			Role: c.role,
			ID:   c.id,
		})

		if c.role == "player1" {
			paddle1 = Paddlemovement{
				X:      0,
				Y:      GAME_HEIGHT / 2,
				Role:   "player1",
				Id:     c.id,
				Width:  PADDLE_WIDTH,
				Height: PADDLE_HEIGHT,
			}
		} else {
			paddle2 = Paddlemovement{
				X:      GAME_WIDTH - PADDLE_WIDTH,
				Y:      GAME_HEIGHT / 2,
				Role:   "player2",
				Id:     c.id,
				Width:  PADDLE_WIDTH,
				Height: PADDLE_HEIGHT,
			}
		}
	}
	clientsMu.Unlock()

	gameMu.Lock()
	game = &Gamestate{
		Type:    "GAME_STATE",
		Ball:    Ballcreation(),
		Paddle1: paddle1,
		Paddle2: paddle2,
		Score:   Score{},
	}
	gameMu.Unlock()

	go Mainlogic(game, stopGame)
}
