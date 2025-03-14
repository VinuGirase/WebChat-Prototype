package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	occupied    = []int{}
	unoccupied  = []int{}
	occupiedApi = []string{}
	lastUsedMap = make(map[int]int64)
	mu          sync.Mutex
)

func AssignNumber() (int, error) {
	mu.Lock()
	defer mu.Unlock()

	if len(unoccupied) == 0 {
		return 0, fmt.Errorf("no available numbers")
	}

	num := unoccupied[0]
	unoccupied = unoccupied[1:]
	occupied = append(occupied, num)
	occupiedApi = append(occupiedApi, fmt.Sprintf("/ws/%d", num))
	lastUsedMap[num] = time.Now().Unix()
	return num, nil
}

func UseNum(num int) {
	mu.Lock()
	defer mu.Unlock()

	if _, exists := lastUsedMap[num]; exists {
		lastUsedMap[num] = time.Now().Unix()
	}
}

func ReleaseNumber(num int) {
	mu.Lock()
	defer mu.Unlock()

	for i, v := range occupied {
		if v == num {
			occupied = append(occupied[:i], occupied[i+1:]...)
			occupiedApi = append(occupiedApi[:i], occupiedApi[i+1:]...)
			unoccupied = append(unoccupied, num)
			delete(lastUsedMap, num)
			fmt.Printf("Released %d | Occupied: %v | Unoccupied: %v\n", num, occupied, unoccupied)
			return
		}
	}

}

func CleanupRoutine() {
	for {
		time.Sleep(time.Hour)
		mu.Lock()
		now := time.Now().Unix()
		var toRelease []int
		for num, lastUsed := range lastUsedMap {
			if now-lastUsed > 300 {
				toRelease = append(toRelease, num)
			}
		}
		mu.Unlock()
		for _, num := range toRelease {
			ReleaseNumber(num)
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var clients = make(map[*websocket.Conn]bool)

type Message struct {
	Username string `json:"username"`
	Message  string `json:"message"`
	Code     int    `json:"code"`
}

func contains(item string) bool {
	for _, value := range occupiedApi {
		if value == item {
			return true
		}
	}
	return false
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	if !contains(r.URL.Path) {
		http.Error(w, "Invalid API", http.StatusNotFound)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()
	clients[conn] = true

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			delete(clients, conn)
			break
		}
		var receivedMessage Message
		err = json.Unmarshal(msg, &receivedMessage)
		if err != nil {
			fmt.Println("Error parsing message:", err)
			continue
		}
		UseNum(receivedMessage.Code)
		for client := range clients {
			client.WriteMessage(websocket.TextMessage, msg)
		}
	}
}

func GenSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID, err := AssignNumber()
	if err != nil {
		http.Error(w, "No available numbers", http.StatusServiceUnavailable)
		return
	}

	response := map[string]string{"session_id": strconv.Itoa(sessionID)}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type Response struct {
	Exists bool `json:"exists"`
}

func checkCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	codeParam := r.URL.Query().Get("code")
	code, err := strconv.Atoi(codeParam)
	if err != nil {
		json.NewEncoder(w).Encode(Response{Exists: false})
		return
	}

	for _, num := range occupied {
		if num == code {
			json.NewEncoder(w).Encode(Response{Exists: true})
			return
		}
	}
	json.NewEncoder(w).Encode(Response{Exists: false})
}

func enableCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
}

func genLink() {
	for i := 1000; i <= 1010; i++ {
		api := fmt.Sprintf("/ws/%d", i)
		unoccupied = append(unoccupied, i)
		http.HandleFunc(api, func(w http.ResponseWriter, r *http.Request) {
			enableCORS(w, r)
			handleConnections(w, r)
		})
	}
}

func main() {
	genLink()

	http.HandleFunc("/genSession", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w, r)
		GenSession(w, r)
	})

	http.HandleFunc("/check-code", func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w, r)
		checkCode(w, r)
	})

	go CleanupRoutine()

	port := ":9218"
	fmt.Println("Server running on http://localhost" + port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Println("Server error:", err)
	}
}
