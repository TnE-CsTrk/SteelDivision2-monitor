package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	sharedConn net.Conn
	connError  error
)

var (
	lastOffset int64 = 0
	oldstamp   int64 = 0
)

var (
	ipstr        = "127.0.0.1"    // 静态IP
	portnum      = "27234"        // 静态端口
	rconpassword = "yourpassword" // 静态密码
	chatfile     = "chat.txt"
	deckfile     = "deck.txt"
	usebot       = false
	modeonly     = true
)

type Player struct {
	EugNetID          string
	PlayerName        string
	SteamID           string
	IPAddress         string
	PlayerAlliance    string
	PlayerDeckContent string
	PlayerType        string // "Human" or "Computer"
	PlayerIALevel     int
}

type DeckEntry struct {
	EugNetID          string
	PlayerName        string
	SteamID           string
	PlayerDeckContent string
	Remark            string // 新增字段，用于存储备注信息
}

// 使用包级别的变量和互斥锁来管理玩家集合
var (
	players      = make(map[string]*Player) // 使用map来存储玩家信息，key为EugNetID
	deckContents [20]DeckEntry              // 存储玩家的 PlayerDeckContent 及相关信息
	mutex        sync.Mutex
	serverName   string
	mapName      string
	totalClients int
)

var validAPIKeys = map[string]bool{
	"p*L*CGwa85hNk)etW^&LWvJ^eqHp>V": true,
	// Add more API keys as needed
}

func main() {
	file, err := os.Open("entrypoint.sh")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	// 创建一个扫描器读取文件
	scannerf := bufio.NewScanner(file)
	for scannerf.Scan() {
		line := scannerf.Text()

		// 使用正则表达式匹配 rcon_password 和 rcon_port
		passwordRegex := regexp.MustCompile(`-rcon_password (\S+)`)
		portRegex := regexp.MustCompile(`-rcon_port (\S+)`)
		chatfileRegex := regexp.MustCompile(`-chat_log_file (\S+)`)

		// 寻找匹配项
		if matches := passwordRegex.FindStringSubmatch(line); matches != nil {
			rconpassword = matches[1]
		}
		if matches := portRegex.FindStringSubmatch(line); matches != nil {
			portnum = matches[1]
		}
		if matches := chatfileRegex.FindStringSubmatch(line); matches != nil {
			chatfile = matches[1]
		}
	}

	// 从自定义文件中加载 DeckEntry 数据
	err = loadDeckEntriesFromCustomFile(deckfile)
	if err != nil {
		fmt.Printf("加载文件时出错: %v\n", err)
		return
	}

	// 输出 deckContents 的内容
	fmt.Println("=== 当前的 Deck Contents ===")
	for i, deck := range deckContents {
		if deck.PlayerName == "" {
			fmt.Printf("槽位 %d: null\n", i+1)
		} else {
			fmt.Printf("槽位 %d: %+v\n", i+1, deck)
		}
	}

	// 检查扫描是否有错误
	if err := scannerf.Err(); err != nil {
		fmt.Println("Error reading file:", err)
	}
	// 输出结果
	fmt.Println("Password:", rconpassword)
	fmt.Println("Port:", portnum)

	go rconcontorl()
	//chatpath := "settings/" + chatfile
	go func() {
		http.HandleFunc("/status", handleStatus)
		http.HandleFunc("/func", handleFunc) //
		http.HandleFunc("/deck", handleCode)
		log.Fatal(http.ListenAndServe(":28323", nil)) //你的api端口
	}()

	// 主线程从通道中接收数据并处理
	go func() {
		lineChan := make(chan string)
		// 启动一个新的 goroutine，传入通道
		go monitorFile(filepath.Join("settings", "chat.txt"), lineChan)
		// 启动处理通道数据的 goroutine
		go processChatLines(lineChan)

	}()

	cmd := exec.Command("sh", "-c", "./entrypoint.sh")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("Error creating stdout pipe:", err)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Println("Error starting command:", err)
		return
	}
	mapName = "default"
	scanner := bufio.NewScanner(stdout)
	var wg sync.WaitGroup
	var nbPlayersAndIA = 0
	var nbIA = 0
	var AutoFillAI = 0
	var doubleAI = 0

	reMAP := regexp.MustCompile(`Variable Map set to "_(.+)"`)
	reNbPlayersAndIA := regexp.MustCompile(`Variable NbPlayersAndIA set to "(\d+)"`)
	reNbIA := regexp.MustCompile(`Variable NbIA set to "(\d+)"`)

	for scanner.Scan() {
		line := scanner.Text()

		if matches := reMAP.FindStringSubmatch(line); len(matches) > 1 {
			mapName = matches[1]
		}

		if matches := reNbPlayersAndIA.FindStringSubmatch(line); len(matches) > 1 {
			nbPlayersAndIA, _ = strconv.Atoi(matches[1])
			totalClients = nbPlayersAndIA
		}

		if matches := reNbIA.FindStringSubmatch(line); len(matches) > 1 {
			nbIA, _ = strconv.Atoi(matches[1])
		}

		if usebot {

			if nbPlayersAndIA > 0 {
				if nbIA >= 2 && AutoFillAI == 1 {
					sendMessage("setsvar AutoFillAI 0", sharedConn)
					AutoFillAI = 0
					fmt.Println("!!!WARNING!!! AutoFillAI Stopped!!!")
				} else if !(nbIA >= 2) && AutoFillAI == 0 && doubleAI == 0 {
					sendMessage("setsvar AutoFillAI 1", sharedConn)
					AutoFillAI = 1
					fmt.Println("!!!WARNING!!! AutoFillAI Starting!!!")
				} else if doubleAI == -1 {
					var bluePlayers []*Player
					var redPlayers []*Player

					for _, player := range players {
						if player.PlayerAlliance != "1" {
							redPlayers = append(redPlayers, player)
						} else {
							bluePlayers = append(bluePlayers, player)
						}
					}
					if len(bluePlayers) < 9 && len(redPlayers) < 9 {
						doubleAI = 0
						fmt.Println("!!!WARNING!!! DoubleAI = 0!!!")
					}
				}
			}

			if nbIA == 2 && doubleAI == 0 {

				var bluePlayers []*Player
				var redPlayers []*Player

				for _, player := range players {
					if player.PlayerAlliance != "1" {
						redPlayers = append(redPlayers, player)
					} else {
						bluePlayers = append(bluePlayers, player)
					}

				}
				var aibluePlayers []string
				var airedPlayers []string

				for _, player := range bluePlayers {
					if player.PlayerType == "Computer" {
						aibluePlayers = append(aibluePlayers, player.EugNetID)
					}
				}

				for _, player := range redPlayers {
					if player.PlayerType == "Computer" {
						airedPlayers = append(airedPlayers, player.EugNetID)
					}
				}

				if len(aibluePlayers) == 2 {
					var kickbot = "kick " + aibluePlayers[1]
					var changebotname = "setpvar " + aibluePlayers[1] + " PlayerName [discord]erJgTT9pWs(reportTK &unBan)"
					if len(redPlayers) == 10 {
						sendMessage(kickbot, sharedConn)
						aibluePlayers = aibluePlayers[:1]
						doubleAI = -1
					} else {
						var movebotred = "setpvar " + aibluePlayers[1] + " PlayerAlliance 0"
						var changedeck = "setpvar " + aibluePlayers[1] + " PlayerDeckContent DCRdmwCCITalIgrUih4VqwNkK1oGyFaqDbHHZBtjjogEBx1QCA47IBAcdUG0OOyDaHHSKKhWoiphx2CFgACwx8K1oY+FagMYHHRGrAAKjWAAEAnwACIT0ABAJ+AAqDICtVKOhWslHQrVQ5oVrIc0K1YokFa0USCtVKKBWslFArVgAoVrQAUK1UCWFayBICtVKHFIMlDikFiiJSDRREpBUP4UgyH8KQZFRgAIliQAFQh4AChCwABEYuOOgOQAAUHIAAA="
						sendMessage(movebotred, sharedConn)
						sendMessage(changebotname, sharedConn)
						sendMessage(changedeck, sharedConn)
						airedPlayers = append(airedPlayers, aibluePlayers[1])
						aibluePlayers = aibluePlayers[:1]
						doubleAI = 1
					}
				} else if len(airedPlayers) == 2 {
					var kickbot = "kick " + airedPlayers[1]
					var changebotname = "setpvar " + airedPlayers[1] + " PlayerName [Q群]429073856(autoFullKickBOT)"
					if len(bluePlayers) == 10 {
						sendMessage(kickbot, sharedConn)
						airedPlayers = airedPlayers[:1]
						doubleAI = -1
					} else {
						var movebotblue = "setpvar " + airedPlayers[1] + " PlayerAlliance 1"
						var changedeck = "setpvar " + airedPlayers[1] + " PlayerDeckContent DCRSFsEEQm0MKAAIkpgAERH4ACRQMABElKAAqInAAWJ/gAMiKwAGigIACBKMABQlGAAwSjAAWEUgAIgugAFQXQADILoABEfIAAqPiAAWKUAAIGFAAEjCgACIA4Wk"
						sendMessage(movebotblue, sharedConn)
						sendMessage(changebotname, sharedConn)
						sendMessage(changedeck, sharedConn)
						aibluePlayers = append(aibluePlayers, airedPlayers[1])
						airedPlayers = airedPlayers[:1]
						doubleAI = 1
					}
					doubleAI = 1
				}
			} else if nbIA == 0 && doubleAI != 0 {
				doubleAI = 0
				fmt.Println("!!!WARNING!!! DoubleAI = 0!!!")
			} else if nbIA == 1 && doubleAI == 1 {
				doubleAI = 0
				fmt.Println("!!!WARNING!!! DoubleAI = 0!!!")
			}
		}
		// 添加新客户端
		if strings.Contains(line, "Client added in session") {
			re := regexp.MustCompile(`EugNetId : (\d+),.*IP : ([\d\.]+:\d+)`)
			match := re.FindStringSubmatch(line)
			if match != nil {
				player := &Player{
					EugNetID:  match[1],
					IPAddress: strings.Split(match[2], ":")[0], // 只取IP，不要端口
				}
				players[match[1]] = player
			}

			//
			// 计数 PlayerAlliance 为 "1" 和 "0" 的玩家数量
			allianceRedCount := 0
			allianceBlueCount := 0

			// 遍历 map
			for _, player := range players {
				if player.PlayerAlliance == "1" {
					allianceBlueCount++
				} else if player.PlayerAlliance == "0" {
					allianceRedCount++
				}
			}
			// 判断并输出结果
			if allianceRedCount > allianceBlueCount {
				players[match[1]].PlayerAlliance = "1"
				fmt.Printf("!!!WARNING!!! 设置玩家al为1blue red: %d blue: %d\n", allianceRedCount, allianceBlueCount)
			} else {
				players[match[1]].PlayerAlliance = "0"
				fmt.Printf("!!!WARNING!!! 设置玩家al为0red red: %d blue: %d\n", allianceRedCount, allianceBlueCount)
			}
		}

		// 设置玩家名
		if strings.Contains(line, "variable PlayerName set to") {
			re := regexp.MustCompile(`Client (\d+) variable PlayerName set to "(.+)"`)
			match := re.FindStringSubmatch(line)
			if match != nil && players[match[1]] != nil {
				players[match[1]].PlayerName = match[2]
			}
		}

		// 设置SteamID
		if strings.Contains(line, "variable PlayerAvatar set to") {
			re := regexp.MustCompile(`Client (\d+) variable PlayerAvatar set to ".*SteamGamerPicture/(\d+)"`)
			match := re.FindStringSubmatch(line)
			if match != nil && players[match[1]] != nil {
				players[match[1]].SteamID = match[2]
			}
		}

		// 设置玩家联盟
		if strings.Contains(line, "variable PlayerAlliance set to") {
			re := regexp.MustCompile(`Client (\d+) variable PlayerAlliance set to "(\d+)"`)
			match := re.FindStringSubmatch(line)
			if match != nil && players[match[1]] != nil {
				players[match[1]].PlayerAlliance = match[2]
			}
		}

		// 设置玩家卡组
		if strings.Contains(line, "variable PlayerDeckContent set to") {
			re := regexp.MustCompile(`Client (\d+) variable PlayerDeckContent set to "(.+)"`)
			match := re.FindStringSubmatch(line)
			if match != nil && players[match[1]] != nil {
				players[match[1]].PlayerDeckContent = match[2]
			}
		}

		// 客户端断开连接
		if strings.Contains(line, "Disconnecting client") {
			re := regexp.MustCompile(`Disconnecting client (\d+)`)
			match := re.FindStringSubmatch(line)
			if match != nil {
				delete(players, match[1])
			}
		}

		// 特殊处理电脑玩家
		if strings.Contains(line, "Client IA added in session") {
			player := &Player{
				PlayerType: "Computer",
			}
			re := regexp.MustCompile(`EugNetId : (\d+),`)
			match := re.FindStringSubmatch(line)
			if match != nil {
				player.EugNetID = match[1]
				players[match[1]] = player
			}
		}

		// 解析 PlayerIALevel 并调整 IPAddress 记录方式
		if strings.Contains(line, "variable PlayerIALevel set to") {
			re := regexp.MustCompile(`Client (\d+) variable PlayerIALevel set to "(\d+)"`)
			match := re.FindStringSubmatch(line)
			if match != nil && players[match[1]] != nil {
				level, _ := strconv.Atoi(match[2])
				players[match[1]].PlayerIALevel = level
				// 设置 IP 地址为难度级别
				players[match[1]].IPAddress = difficultyLevelToIP(level)
				players[match[1]].SteamID = "AI"
			}
		}

		// 电脑断开连接
		if strings.Contains(line, "Disconnecting IA") {
			re := regexp.MustCompile(`Disconnecting IA (\d+)`)
			match := re.FindStringSubmatch(line)
			if match != nil {
				delete(players, match[1])
			}
		}

		wg.Add(1)
		go func(data string) {
			defer wg.Done()
			Log(data)
		}(line)
	}

	wg.Wait()
	if err := cmd.Wait(); err != nil {
		fmt.Println("Command finished with error:", err)
	}
}
func atoi(s string) int {
	i, _ := strconv.Atoi(s)
	return i
}

func difficultyLevelToIP(level int) string {
	difficulties := map[int]string{
		6: "Extremely Hard",
		5: "Very Hard",
		4: "Hard",
		3: "Medium",
		2: "Easy",
		1: "Very Easy",
	}
	return difficulties[level]
}

func Log(data string) {
	// 假设的日志处理逻辑
	timestamp := time.Now().Unix()
	// 再格式化时间戳转化为日期
	datetime := time.Unix(timestamp, 0).Format("2006-01-02 15:04:05")
	fmt.Println(datetime, data)
}

func rconcontorl() {
	time.Sleep(2 * time.Second) //等待服务器启动
	// 创建TCP连接
	sharedConn, connError = net.DialTimeout("tcp", net.JoinHostPort(ipstr, portnum), 2*time.Second)
	if connError != nil {
		fmt.Println("Connection Timeout or Error:", connError)
		return
	}
	defer sharedConn.Close()

	// 发送身份验证信息
	connError = sendAuth(sharedConn)
	if connError != nil {
		fmt.Println("Error sending auth:", connError)
		return
	}
	fmt.Println("!!!WARNING!!! Rcon establish connect successfully!!!")

	// 创建一个读取器，从标准输入读取
	reader := bufio.NewReader(os.Stdin)

	// 实时读取并发送指令
	for {
		message, _ := reader.ReadString('\n')
		message = strings.TrimSpace(message) // 移除字符串的头尾空白字符

		switch message {
		case "quit":
			os.Exit(0) // 关闭go程序 .sh脚本仍在运行
		case "status":
			var bluePlayers []*Player
			var redPlayers []*Player

			for _, player := range players {
				if player.PlayerAlliance != "1" {
					redPlayers = append(redPlayers, player)
				} else {
					bluePlayers = append(bluePlayers, player)
				}
			}
			// Sort players by EugNetID for consistent ordering
			sort.Slice(bluePlayers, func(i, j int) bool {
				return bluePlayers[i].EugNetID < bluePlayers[j].EugNetID
			})
			sort.Slice(redPlayers, func(i, j int) bool {
				return redPlayers[i].EugNetID < redPlayers[j].EugNetID
			})

			fmt.Printf("Map : %s TotalClients : %d \n", mapName, totalClients)
			// Output BLUE players
			fmt.Printf("BLUE (%d Players)\n", len(bluePlayers))
			for index, player := range bluePlayers {
				fmt.Printf("#%d %s %s %s %s\n Deck: %s\n", index+1, player.PlayerName, player.EugNetID, player.SteamID, player.IPAddress, player.PlayerDeckContent)
			}

			// Output RED players
			fmt.Printf("RED (%d Players)\n", len(redPlayers))
			for index, player := range redPlayers {
				fmt.Printf("#%d %s %s %s %s\n Deck: %s\n", index+1, player.PlayerName, player.EugNetID, player.SteamID, player.IPAddress, player.PlayerDeckContent)
			}
			continue
		case "deck":
			fmt.Println("\n=== 所有位置的玩家卡组信息 ===")
			for i, deck := range deckContents {
				position := i + 1
				if deck.PlayerDeckContent == "" {
					fmt.Printf("位置 %d: null\n", position)
				} else {
					fmt.Printf("位置 %d:\n", position)
					fmt.Printf("  EugNetID: %s\n", deck.EugNetID)
					fmt.Printf("  PlayerName: %s\n", deck.PlayerName)
					fmt.Printf("  SteamID: %s\n", deck.SteamID)
					fmt.Printf("  PlayerDeckContent: %s\n", deck.PlayerDeckContent)
					if deck.Remark != "" {
						fmt.Printf("  Remark: %s\n", deck.Remark)
					} else {
						fmt.Printf("  Remark: null\n")
					}
				}
			}
			continue
		case "version":
			fmt.Println("Steel Division 2 monitor Version 1.10 ")
			continue
		}

		connError = sendMessage(message, sharedConn)
		if connError != nil {
			fmt.Println("Error sending message:", connError)
			return
		}
		fmt.Println("!!!WARNING!!! Message sent successfully!!!")
	}
}

func sendAuth(conn net.Conn) error {
	var buf bytes.Buffer
	pw := binary.Write(&buf, binary.LittleEndian, int32(len(rconpassword)+10))
	pw = binary.Write(&buf, binary.LittleEndian, int32(100))
	pw = binary.Write(&buf, binary.LittleEndian, int32(3))
	pw = binary.Write(&buf, binary.LittleEndian, []byte(rconpassword))
	pw = binary.Write(&buf, binary.LittleEndian, byte(0))
	pw = binary.Write(&buf, binary.LittleEndian, byte(0))

	if pw != nil {
		return pw
	}
	_, err := conn.Write(buf.Bytes())
	return err
}

func sendMessage(message string, conn net.Conn) error {
	var buff bytes.Buffer

	// 构建数据包
	pw := binary.Write(&buff, binary.LittleEndian, int32(len(message)+10))
	pw = binary.Write(&buff, binary.LittleEndian, int32(42))
	pw = binary.Write(&buff, binary.LittleEndian, int32(2))
	pw = binary.Write(&buff, binary.LittleEndian, []byte(message))
	pw = binary.Write(&buff, binary.LittleEndian, byte(0))
	pw = binary.Write(&buff, binary.LittleEndian, byte(0))

	if pw != nil {
		return pw
	}

	_, err := conn.Write(buff.Bytes())
	if err != nil {
		// Handle the error
		fmt.Println("Error sending message:", err)

		// Optionally attempt to reconnect here
		// Example: Close existing connection and reconnect
		conn.Close()
		sharedConn, connError = net.DialTimeout("tcp", net.JoinHostPort(ipstr, portnum), 2*time.Second)
		if connError != nil {
			fmt.Println("Error reconnecting:", connError)
			return connError
		}

		// Re-authenticate if necessary
		connError = sendAuth(sharedConn)
		if connError != nil {
			fmt.Println("Error re-authenticating:", connError)
			return connError
		}
		fmt.Println("!!!WARNING!!! ReSend Message")
		// Retry sending the message after reconnecting
		_, err = sharedConn.Write(buff.Bytes())
		if err != nil {
			fmt.Println("Error retry sending message:", err)
			return err
		}
	}
	return nil
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	// 获取apikey参数
	apiKey := r.Header.Get("apikey")
	// 检查apikey的有效性
	if !validAPIKeys[apiKey] {
		http.Error(w, "Invalid API Key", http.StatusUnauthorized)
		return
	}

	var bluePlayers []*Player
	var redPlayers []*Player
	totalClients := len(players)

	for _, player := range players {
		if player.PlayerAlliance != "1" {
			redPlayers = append(redPlayers, player)
		} else {
			bluePlayers = append(bluePlayers, player)
		}
	}

	// Sort players by EugNetID for consistent ordering
	sort.Slice(bluePlayers, func(i, j int) bool {
		return bluePlayers[i].EugNetID < bluePlayers[j].EugNetID
	})
	sort.Slice(redPlayers, func(i, j int) bool {
		return redPlayers[i].EugNetID < redPlayers[j].EugNetID
	})

	// Prepare response data
	responseData := map[string]interface{}{
		"map_name":      mapName,
		"total_clients": totalClients,
		"blue_players":  bluePlayers,
		"red_players":   redPlayers,
	}

	// Convert response data to JSON
	jsonResponse, err := json.Marshal(responseData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set Content-Type header and write JSON response
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}

func handleCode(w http.ResponseWriter, r *http.Request) {
	// 获取apikey参数
	apiKey := r.Header.Get("apikey")
	// 检查apikey的有效性
	if !validAPIKeys[apiKey] {
		http.Error(w, "Invalid API Key", http.StatusUnauthorized)
		return
	}

	// 按照数组顺序获取 DeckEntry，并加上编号 (index + 1)
	var sortedDeckEntries []interface{}
	for i, entry := range deckContents {
		// 检查数组中的零值，通过判断 EugNetID 是否为空
		if entry.EugNetID != "" {
			// 如果插槽不为空，则包括实际数据和 index
			sortedDeckEntries = append(sortedDeckEntries, map[string]interface{}{
				"index":               i + 1, // 显示第几个
				"eug_net_id":          entry.EugNetID,
				"player_name":         entry.PlayerName,
				"steam_id":            entry.SteamID,
				"player_deck_content": entry.PlayerDeckContent,
				"remark":              entry.Remark,
			})
		} else {
			// 如果插槽为空，添加 nil
			sortedDeckEntries = append(sortedDeckEntries, nil)
		}
	}

	// 准备返回的数据
	responseData := map[string]interface{}{
		"deck_entries": sortedDeckEntries,
	}

	// 将数据转换为 JSON 格式
	jsonResponse, err := json.Marshal(responseData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 设置响应头和返回数据
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}

func handleFunc(w http.ResponseWriter, r *http.Request) {

	// 获取apikey参数
	apiKey := r.Header.Get("apikey")
	// 检查apikey的有效性
	if !validAPIKeys[apiKey] {
		http.Error(w, "Invalid API Key", http.StatusUnauthorized)
		return
	}

	// 解析参数
	params := r.URL.Query()
	funcParam := params.Get("") // 获取等号后的参数内容

	// 根据参数内容执行不同的功能
	switch funcParam {
	case "map1":
		sendMessage("setsvar Map _4x2_Ostrowno_LD_4v4", sharedConn)
		fmt.Fprintf(w, "地图更换为Ostrowno(4v4)")
	case "map2":
		sendMessage("setsvar Map _4x2_Lenina_LD_4v4", sharedConn)
		fmt.Fprintf(w, "地图更换为Lenina(4v4)")
	case "map3":
		sendMessage("setsvar Map _4x2_Shchedrin_LD_4v4", sharedConn)
		fmt.Fprintf(w, "地图更换为Shchedrin(4v4)")
	case "map4":
		sendMessage("setsvar Map _4x2_Vistula_Gora_Kalwaria_LD_4v4", sharedConn)
		fmt.Fprintf(w, "地图更换为Gora_Kalwaria(4v4)")
	case "map5":
		if !modeonly {
			sendMessage("setsvar Map _2x1_Proto_levelBuild_Orsha_N_LD_1v1", sharedConn)
			fmt.Fprintf(w, "地图更换为Orsha_North(1v1)")
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换1v1地图")
		}
	case "map6":
		if !modeonly {
			sendMessage("setsvar Map _2x2_Plateau_Central_Orsha_E_LD_1v1", sharedConn)
			fmt.Fprintf(w, "地图更换为Orsha_East(1v1)")
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换1v1地图")
		}
	case "map7":
		if !modeonly {
			sendMessage("setsvar Map _2x2_Slutsk_E_LD_1v1", sharedConn)
			fmt.Fprintf(w, "地图更换为Slutsk_East(1v1)")
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换1v1地图")
		}
	case "map8":
		if !modeonly {
			sendMessage("setsvar Map _2x2_Kostritsa_LD_1v1", sharedConn)
			fmt.Fprintf(w, "地图更换为Kostritsa(1v1)")
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换1v1地图")
		}
	case "map9":
		if !modeonly {
			sendMessage("setsvar Map _2x2_Shchedrin_LD_1v1", sharedConn)
			fmt.Fprintf(w, "地图更换为Shchedrin(1v1)")
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换1v1地图")
		}
	case "map10":
		if !modeonly {
			sendMessage("setsvar Map _4x2_Tannenberg_LD_1v1", sharedConn)
			fmt.Fprintf(w, "地图更换为Tannenberg(1v1)")
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换1v1地图")
		}
	case "map11":
		if !modeonly {
			sendMessage("setsvar Map _2x2_Urban_River_Bobr_LD_1v1", sharedConn)
			fmt.Fprintf(w, "地图更换为Bobr(1v1)")
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换1v1地图")
		}
	case "map12":
		if !modeonly {
			sendMessage("setsvar Map _2x2_Ville_Centrale_Haroshaje_LD_1v1", sharedConn)
			fmt.Fprintf(w, "地图更换为Haroshaje(1v1)")
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换1v1地图")
		}
	case "mode10x":
		sendMessage("setsvar InitMoney 750", sharedConn)
		sendMessage("setsvar IncomeRate 3", sharedConn)
		sendMessage("setsvar NbMaxPlayer 20", sharedConn)
		sendMessage("setsvar NbMinPlayer 20", sharedConn)
		sendMessage("setsvar DeltaMaxTeamSize 10", sharedConn)
		sendMessage("setsvar MaxTeamSize 10", sharedConn)
		fmt.Fprintf(w, "切换为1x倍率10v10模式")
	case "mode10c":
		sendMessage("setsvar InitMoney 250", sharedConn)
		sendMessage("setsvar IncomeRate 1", sharedConn)
		sendMessage("setsvar NbMaxPlayer 20", sharedConn)
		sendMessage("setsvar NbMinPlayer 20", sharedConn)
		sendMessage("setsvar DeltaMaxTeamSize 10", sharedConn)
		sendMessage("setsvar MaxTeamSize 10", sharedConn)
		fmt.Fprintf(w, "切换为战术10v10模式")
	case "mode10":
		sendMessage("setsvar InitMoney 500", sharedConn)
		sendMessage("setsvar IncomeRate 2", sharedConn)
		sendMessage("setsvar NbMaxPlayer 20", sharedConn)
		sendMessage("setsvar NbMinPlayer 20", sharedConn)
		sendMessage("setsvar DeltaMaxTeamSize 10", sharedConn)
		sendMessage("setsvar MaxTeamSize 10", sharedConn)
		fmt.Fprintf(w, "切换为标准10v10模式")
	case "mode4":
		if !modeonly {
			if len(players) > 8 {
				fmt.Fprintf(w, "玩家数大于8人！当前不支持切换标准4v4模式")
			} else {
				sendMessage("setsvar InitMoney 750", sharedConn)
				sendMessage("setsvar IncomeRate 3", sharedConn)
				sendMessage("setsvar NbMaxPlayer 8", sharedConn)
				sendMessage("setsvar NbMinPlayer 8", sharedConn)
				sendMessage("setsvar DeltaMaxTeamSize 4", sharedConn)
				sendMessage("setsvar MaxTeamSize 4", sharedConn)
				fmt.Fprintf(w, "切换为标准4v4模式")
			}
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换标准4v4模式")
		}
	case "mode2":
		if !modeonly {
			if len(players) > 4 {
				fmt.Fprintf(w, "玩家数大于4人！当前不支持切换合作2v2模式")
			} else {
				sendMessage("setsvar InitMoney 500", sharedConn)
				sendMessage("setsvar IncomeRate 2", sharedConn)
				sendMessage("setsvar NbMaxPlayer 4", sharedConn)
				sendMessage("setsvar NbMinPlayer 4", sharedConn)
				sendMessage("setsvar DeltaMaxTeamSize 2", sharedConn)
				sendMessage("setsvar MaxTeamSize 2", sharedConn)
				fmt.Fprintf(w, "切换为合作2v2模式(1v1地图)")
			}
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换合作2v2模式")
		}
	case "mode1":
		if !modeonly {
			if len(players) > 2 {
				fmt.Fprintf(w, "玩家数大于2人！当前不支持切换标准1v1模式")
			} else {
				sendMessage("setsvar InitMoney 750", sharedConn)
				sendMessage("setsvar IncomeRate 3", sharedConn)
				sendMessage("setsvar NbMaxPlayer 2", sharedConn)
				sendMessage("setsvar NbMinPlayer 2", sharedConn)
				sendMessage("setsvar DeltaMaxTeamSize 1", sharedConn)
				sendMessage("setsvar MaxTeamSize 1", sharedConn)
				fmt.Fprintf(w, "切换为标准1v1模式")
			}
		} else {
			fmt.Fprintf(w, "已锁定模式！当前不支持切换标准1v1模式")
		}
	case "botrun":
		// 执行功能2的代码
		fmt.Fprintf(w, "已开启暖服bot")
		usebot = true
	case "botstop":
		// 执行功能2的代码
		fmt.Fprintf(w, "已关闭暖服bot")
		usebot = false
		for _, player := range players {
			if player.PlayerType == "Computer" {
				var kickbot = "kick " + player.PlayerName
				sendMessage(kickbot, sharedConn)
			}
		}
	default:
		http.Error(w, "未知功能", http.StatusBadRequest)
	}
}

func monitorFile(fileName string, lineChan chan<- string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("无法创建文件监视器:", err)
		return
	}
	defer watcher.Close()

	err = watcher.Add(fileName)
	if err != nil {
		fmt.Println("无法监视文件:", err)
		return
	}

	file, err := os.Open(fileName)
	if err != nil {
		fmt.Println("无法打开文件:", err)
		return
	}
	defer file.Close()

	// 将文件指针移动到文件末尾
	_, err = file.Seek(0, io.SeekEnd)
	if err != nil {
		fmt.Println("无法移动文件指针:", err)
		return
	}

	reader := bufio.NewReader(file)

	for {
		select {
		case event := <-watcher.Events:
			if event.Op&fsnotify.Write == fsnotify.Write {
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						if err == io.EOF {
							break
						} else {
							fmt.Println("读取文件出错:", err)
							return
						}
					}
					line = strings.TrimSpace(line)
					if line != "" {
						lineChan <- line
					}
				}
			}
		case err := <-watcher.Errors:
			fmt.Println("监视器错误:", err)
		}
	}
}

func processChatLines(lineChan <-chan string) {
	var processedMessages = make(map[string]bool)

	for line := range lineChan {

		fmt.Print("\n收到新行: ", line)
		// 解析行内容
		timestamp, playerID, message, parseErr := parseChatLine(line)
		if parseErr != nil {
			fmt.Println("解析聊天行出错:", parseErr)
			continue
		}

		// 构建唯一键
		messageKey := fmt.Sprintf("%s-%s-%s", timestamp, playerID, message)
		if processedMessages[messageKey] {
			continue // 已处理过，跳过
		}
		processedMessages[messageKey] = true

		// 检查并处理特定命令
		handleChatCommand(playerID, message)
	}
}

func parseChatLine(line string) (timestamp string, playerID string, message string, err error) {
	// 使用正则表达式匹配行格式
	re := regexp.MustCompile(`\[(\d+)\]\s+(\d+):\s+(.+)`)
	matches := re.FindStringSubmatch(line)
	if matches == nil || len(matches) != 4 {
		return "", "", "", fmt.Errorf("行格式不正确: %s", line)
	}
	timestamp = matches[1]
	playerID = matches[2]
	message = matches[3]
	return timestamp, playerID, message, nil
}

func handleChatCommand(playerID string, message string) {
	// 检查是否是 upload 空格 数字 或 card 空格 数字
	uploadRe := regexp.MustCompile(`^upload\s+(\d+)(?:\s+(.*))?$`)
	cardRe := regexp.MustCompile(`^card\s+(\d+)$`)

	if matches := uploadRe.FindStringSubmatch(message); matches != nil {
		number := matches[1]
		remark := ""
		if len(matches) == 3 {
			remark = matches[2]
		}
		// 调用处理 upload 命令的函数，并传递备注信息
		handleUploadCommand(playerID, number, remark)
	} else if matches := cardRe.FindStringSubmatch(message); matches != nil {
		number := matches[1]
		// 调用处理 card 命令的函数
		handleCardCommand(playerID, number)
	} else {
		// 非命令消息，可以根据需要处理
		fmt.Printf("玩家 %s 发送了消息：%s\n", playerID, message)
	}
}

func handleUploadCommand(playerID string, numberStr string, remark string) {
	// 将数字字符串转换为整数
	number, err := strconv.Atoi(numberStr)
	if err != nil || number < 1 || number > 20 {
		fmt.Printf("玩家 %s 提供的数字无效：%s\n", playerID, numberStr)
		return
	}

	// 查找玩家
	player, exists := players[playerID]
	if !exists {
		fmt.Printf("未找到玩家 %s\n", playerID)
		return
	}

	// 检查 PlayerDeckContent 是否为空
	if player.PlayerDeckContent == "" {
		fmt.Printf("玩家 %s 的 PlayerDeckContent 为空，无法上传\n", playerID)
		return
	}

	// 根据 PlayerAlliance 进行槽位映射
	mappedNumber := number
	if player.PlayerAlliance == "1" {
		// 只能存储到11-20槽位，自动转换1-10为11-20
		if number >= 11 {
			mappedNumber -= 10
			fmt.Printf("玩家 %s 的 PlayerAlliance 为1，槽位 %d 自动转换为 %d\n", playerID, number, mappedNumber)
		}
	} else if player.PlayerAlliance == "0" {
		// 只能存储到1-10槽位，自动转换11-20为1-10
		if number <= 10 {
			mappedNumber += 10
			fmt.Printf("玩家 %s 的 PlayerAlliance 为0，槽位 %d 自动转换为 %d\n", playerID, number, mappedNumber)
		}
	} else {
		fmt.Printf("玩家 %s 的 PlayerAlliance 值无效：%d\n", playerID, player.PlayerAlliance)
		return
	}

	// 验证转换后的槽位是否在允许范围内
	if mappedNumber < 1 || mappedNumber > 20 {
		fmt.Printf("玩家 %s 的槽位 %d 转换后无效：%d\n", playerID, number, mappedNumber)
		return
	}

	// 存储 DeckEntry 到数组中
	deckContents[mappedNumber-1] = DeckEntry{
		EugNetID:          player.EugNetID,
		PlayerName:        player.PlayerName,
		SteamID:           player.SteamID,
		PlayerDeckContent: player.PlayerDeckContent,
		Remark:            remark,
	}
	fmt.Printf("已存储玩家 %s 的 PlayerDeckContent 到位置 %d\n", playerID, mappedNumber)

	// 构建命令字符串
	command := fmt.Sprintf("setpvar %s PlayerDeckContent %s", playerID, player.PlayerDeckContent)

	// 发送命令并进行错误处理
	err = sendMessage(command, sharedConn)
	if err != nil {
		fmt.Printf("发送命令时出错：%v\n", err)
		return
	}
	fmt.Println("Message sent successfully!")
}

func handleCardCommand(playerID string, numberStr string) {
	// 将数字字符串转换为整数
	number, err := strconv.Atoi(numberStr)
	if err != nil || number < 1 || number > 20 {
		fmt.Printf("玩家 %s 提供的数字无效：%s\n", playerID, numberStr)
		return
	}

	// 从数组中获取 DeckEntry
	deckEntry := deckContents[number-1]
	if deckEntry.PlayerDeckContent == "" {
		fmt.Printf("位置 %d 的卡组内容为空，无法执行 card 命令\n", number)
		return
	}

	// 查找玩家
	player, exists := players[playerID]
	if !exists {
		fmt.Printf("未找到玩家 %s\n", playerID)
		return
	}

	// 对玩家执行操作，例如更新玩家的 PlayerDeckContent
	player.PlayerDeckContent = deckEntry.PlayerDeckContent
	fmt.Printf("已将位置 %d 的卡组内容应用到玩家 %s\n", number, playerID)
	// 构建命令字符串
	command := fmt.Sprintf("setpvar %s PlayerDeckContent %s", playerID, deckEntry.PlayerDeckContent)

	// 发送命令并进行错误处理
	err = sendMessage(command, sharedConn)
	if err != nil {
		fmt.Printf("发送命令时出错：%v\n", err)
		return
	}
	fmt.Println("Message sent successfully!")
}

// 使用正则表达式匹配引号包裹的字段
var entryPattern = regexp.MustCompile(`(\S+)\s+(\d+)\s+"([^"]+)"\s+(\S+)\s+(\S+)\s+"([^"]+)"`)

// 从自定义格式的文件中加载数据
func loadDeckEntriesFromCustomFile(filePath string) error {
	// 打开文件

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("文件 %s 不存在，跳过文件读取。\n", filePath)
		return nil // 文件不存在，直接返回，继续程序执行
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("无法打开文件: %v", err)
	}
	defer file.Close()

	// 使用 bufio.Scanner 逐行读取文件
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// 使用正则表达式匹配每一行数据
		matches := entryPattern.FindStringSubmatch(line)
		if matches == nil || len(matches) != 7 {
			fmt.Printf("无效的行格式: %s\n", line)
			continue
		}

		// 解析 Index 字段（确定数据存储位置）
		index, err := strconv.Atoi(matches[2])
		if err != nil || index < 1 || index > 20 {
			fmt.Printf("无效的索引: %s\n", matches[2])
			continue
		}

		// 提取其他字段
		eugNetID := matches[1]
		playerName := matches[3] // 引号内的 PlayerName
		steamID := matches[4]
		playerDeckContent := matches[5]
		remark := matches[6] // 引号内的 Remark

		// 存储到 deckContents 数组（不将 index 存储到结构体中）
		deckContents[index-1] = DeckEntry{
			EugNetID:          eugNetID,
			PlayerName:        playerName,
			SteamID:           steamID,
			PlayerDeckContent: playerDeckContent,
			Remark:            remark,
		}

		fmt.Printf("存储 DeckEntry 到槽位 %d: %+v\n", index, deckContents[index-1])
	}

	// 检查扫描过程中的错误
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("扫描文件时出错: %v", err)
	}

	return nil
}
