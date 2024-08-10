package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
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
  // ！！！！！！！！！！rcon参数配置
	ipstr        = "127.0.0.1"    // 静态IP
	portnum      = "7890"         // 静态端口
	rconpassword = "yourpassword" // 静态密码
	chatfile     = "chat.txt"
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

// 使用包级别的变量和互斥锁来管理玩家集合
var (
	players      = make(map[string]*Player) // 使用map来存储玩家信息，key为EugNetID
	mutex        sync.Mutex
	serverName   string
	mapName      string
	totalClients int
)

var validAPIKeys = map[string]bool{
	"p*L*CGwa85hNk)etW^nkfjckos730X": true,
	// /！！！！！！！！！！修改成你的apikey/！！！！！！！！！！
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
		http.HandleFunc("/func", handleFunc)          //
		log.Fatal(http.ListenAndServe(":27291", nil)) //！！！！！！！！！！修改为你的api端口/！！！！！！！！！！
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
					var changebotname = "setpvar " + aibluePlayers[1] + " PlayerName yourbotname"
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
					var changebotname = "setpvar " + airedPlayers[1] + " PlayerName yourbotname"
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
		case "version":
			fmt.Println("Steel Division 2 monitor Version 1.03 ")
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
