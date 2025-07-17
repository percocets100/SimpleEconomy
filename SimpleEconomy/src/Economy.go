package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type EconomyPlugin struct {
	name        string
	version     string
	dataFolder  string
	playerData  map[string]*PlayerAccount
	mutex       sync.RWMutex
	config      *Config
	topPlayers  []*PlayerAccount
}

type PlayerAccount struct {
	Username    string    `json:"username"`
	Balance     float64   `json:"balance"`
	LastSeen    time.Time `json:"last_seen"`
	TotalEarned float64   `json:"total_earned"`
	TotalSpent  float64   `json:"total_spent"`
}

type Config struct {
	DefaultBalance  float64 `json:"default_balance"`
	MaxBalance      float64 `json:"max_balance"`
	CurrencySymbol  string  `json:"currency_symbol"`
	CurrencyName    string  `json:"currency_name"`
	EnableLogging   bool    `json:"enable_logging"`
	TopPlayersLimit int     `json:"top_players_limit"`
}

type TransactionType int

const (
	ADD TransactionType = iota
	SUBTRACT
	SET
	TRANSFER
)

type Transaction struct {
	From      string          `json:"from"`
	To        string          `json:"to"`
	Amount    float64         `json:"amount"`
	Type      TransactionType `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Reason    string          `json:"reason"`
}

func NewEconomyPlugin() *EconomyPlugin {
	return &EconomyPlugin{
		name:       "EconomyPocketmine",
		version:    "1.0.0",
		dataFolder: "plugins/EconomyPocketmine",
		playerData: make(map[string]*PlayerAccount),
		config: &Config{
			DefaultBalance:  1000.0,
			MaxBalance:      1000000.0,
			CurrencySymbol:  "$",
			CurrencyName:    "Coins",
			EnableLogging:   true,
			TopPlayersLimit: 10,
		},
	}
}

func (e *EconomyPlugin) OnEnable() {
	fmt.Printf("[%s] Enabling %s v%s\n", e.name, e.name, e.version)
	
	if err := os.MkdirAll(e.dataFolder, 0755); err != nil {
		log.Printf("Failed to create data folder: %v", err)
		return
	}
	
	e.loadConfig()
	e.loadPlayerData()
	e.registerCommands()
	
	fmt.Printf("[%s] Plugin enabled successfully!\n", e.name)
}

func (e *EconomyPlugin) OnDisable() {
	fmt.Printf("[%s] Disabling plugin...\n", e.name)
	e.savePlayerData()
	fmt.Printf("[%s] Plugin disabled!\n", e.name)
}

func (e *EconomyPlugin) loadConfig() {
	configPath := filepath.Join(e.dataFolder, "config.json")
	
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		e.saveConfig()
		return
	}
	
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Printf("Failed to read config: %v", err)
		return
	}
	
	if err := json.Unmarshal(data, e.config); err != nil {
		log.Printf("Failed to parse config: %v", err)
	}
}

func (e *EconomyPlugin) saveConfig() {
	configPath := filepath.Join(e.dataFolder, "config.json")
	
	data, err := json.MarshalIndent(e.config, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal config: %v", err)
		return
	}
	
	if err := ioutil.WriteFile(configPath, data, 0644); err != nil {
		log.Printf("Failed to write config: %v", err)
	}
}

func (e *EconomyPlugin) loadPlayerData() {
	dataPath := filepath.Join(e.dataFolder, "players.json")
	
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		return
	}
	
	data, err := ioutil.ReadFile(dataPath)
	if err != nil {
		log.Printf("Failed to read player data: %v", err)
		return
	}
	
	if err := json.Unmarshal(data, &e.playerData); err != nil {
		log.Printf("Failed to parse player data: %v", err)
	}
	
	e.updateTopPlayers()
}

func (e *EconomyPlugin) savePlayerData() {
	dataPath := filepath.Join(e.dataFolder, "players.json")
	
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	
	data, err := json.MarshalIndent(e.playerData, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal player data: %v", err)
		return
	}
	
	if err := ioutil.WriteFile(dataPath, data, 0644); err != nil {
		log.Printf("Failed to write player data: %v", err)
	}
}

func (e *EconomyPlugin) createAccount(username string) *PlayerAccount {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	
	account := &PlayerAccount{
		Username:    username,
		Balance:     e.config.DefaultBalance,
		LastSeen:    time.Now(),
		TotalEarned: e.config.DefaultBalance,
		TotalSpent:  0,
	}
	
	e.playerData[strings.ToLower(username)] = account
	e.updateTopPlayers()
	
	return account
}

func (e *EconomyPlugin) getAccount(username string) *PlayerAccount {
	e.mutex.RLock()
	account, exists := e.playerData[strings.ToLower(username)]
	e.mutex.RUnlock()
	
	if !exists {
		account = e.createAccount(username)
	} else {
		account.LastSeen = time.Now()
	}
	
	return account
}

func (e *EconomyPlugin) getBalance(username string) float64 {
	account := e.getAccount(username)
	return account.Balance
}

func (e *EconomyPlugin) setBalance(username string, amount float64) bool {
	if amount < 0 || amount > e.config.MaxBalance {
		return false
	}
	
	account := e.getAccount(username)
	
	e.mutex.Lock()
	oldBalance := account.Balance
	account.Balance = amount
	e.mutex.Unlock()
	
	e.updateTopPlayers()
	
	if e.config.EnableLogging {
		transaction := &Transaction{
			To:        username,
			Amount:    amount,
			Type:      SET,
			Timestamp: time.Now(),
			Reason:    "Balance set by admin",
		}
		e.logTransaction(transaction)
	}
	
	return true
}

func (e *EconomyPlugin) addMoney(username string, amount float64) bool {
	if amount <= 0 {
		return false
	}
	
	account := e.getAccount(username)
	
	e.mutex.Lock()
	newBalance := account.Balance + amount
	
	if newBalance > e.config.MaxBalance {
		e.mutex.Unlock()
		return false
	}
	
	account.Balance = newBalance
	account.TotalEarned += amount
	e.mutex.Unlock()
	
	e.updateTopPlayers()
	
	if e.config.EnableLogging {
		transaction := &Transaction{
			To:        username,
			Amount:    amount,
			Type:      ADD,
			Timestamp: time.Now(),
			Reason:    "Money added",
		}
		e.logTransaction(transaction)
	}
	
	return true
}

func (e *EconomyPlugin) subtractMoney(username string, amount float64) bool {
	if amount <= 0 {
		return false
	}
	
	account := e.getAccount(username)
	
	e.mutex.Lock()
	if account.Balance < amount {
		e.mutex.Unlock()
		return false
	}
	
	account.Balance -= amount
	account.TotalSpent += amount
	e.mutex.Unlock()
	
	e.updateTopPlayers()
	
	if e.config.EnableLogging {
		transaction := &Transaction{
			From:      username,
			Amount:    amount,
			Type:      SUBTRACT,
			Timestamp: time.Now(),
			Reason:    "Money subtracted",
		}
		e.logTransaction(transaction)
	}
	
	return true
}

func (e *EconomyPlugin) transferMoney(from, to string, amount float64) bool {
	if amount <= 0 || strings.ToLower(from) == strings.ToLower(to) {
		return false
	}
	
	fromAccount := e.getAccount(from)
	toAccount := e.getAccount(to)
	
	e.mutex.Lock()
	if fromAccount.Balance < amount {
		e.mutex.Unlock()
		return false
	}
	
	if toAccount.Balance+amount > e.config.MaxBalance {
		e.mutex.Unlock()
		return false
	}
	
	fromAccount.Balance -= amount
	fromAccount.TotalSpent += amount
	toAccount.Balance += amount
	toAccount.TotalEarned += amount
	e.mutex.Unlock()
	
	e.updateTopPlayers()
	
	if e.config.EnableLogging {
		transaction := &Transaction{
			From:      from,
			To:        to,
			Amount:    amount,
			Type:      TRANSFER,
			Timestamp: time.Now(),
			Reason:    "Money transfer",
		}
		e.logTransaction(transaction)
	}
	
	return true
}

func (e *EconomyPlugin) updateTopPlayers() {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	
	players := make([]*PlayerAccount, 0, len(e.playerData))
	for _, account := range e.playerData {
		players = append(players, account)
	}
	
	for i := 0; i < len(players); i++ {
		for j := 0; j < len(players)-1-i; j++ {
			if players[j].Balance < players[j+1].Balance {
				players[j], players[j+1] = players[j+1], players[j]
			}
		}
	}
	
	limit := e.config.TopPlayersLimit
	if len(players) < limit {
		limit = len(players)
	}
	
	e.topPlayers = players[:limit]
}

func (e *EconomyPlugin) logTransaction(transaction *Transaction) {
	logPath := filepath.Join(e.dataFolder, "transactions.log")
	
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open transaction log: %v", err)
		return
	}
	defer file.Close()
	
	logEntry := fmt.Sprintf("[%s] %s -> %s: %s%.2f (Type: %d, Reason: %s)\n",
		transaction.Timestamp.Format("2006-01-02 15:04:05"),
		transaction.From,
		transaction.To,
		e.config.CurrencySymbol,
		transaction.Amount,
		transaction.Type,
		transaction.Reason)
	
	file.WriteString(logEntry)
}

func (e *EconomyPlugin) formatMoney(amount float64) string {
	return fmt.Sprintf("%s%.2f", e.config.CurrencySymbol, amount)
}

func (e *EconomyPlugin) registerCommands() {
	fmt.Printf("[%s] Registering commands...\n", e.name)
	
	commands := map[string]func([]string) string{
		"balance": e.balanceCommand,
		"money":   e.moneyCommand,
		"pay":     e.payCommand,
		"bal":     e.balanceCommand,
		"economy": e.economyCommand,
		"eco":     e.economyCommand,
		"top":     e.topCommand,
	}
	
	for cmd, handler := range commands {
		fmt.Printf("[%s] Registered command: %s\n", e.name, cmd)
		_ = handler
	}
}

func (e *EconomyPlugin) balanceCommand(args []string) string {
	if len(args) == 0 {
		return "Usage: /balance [player]"
	}
	
	username := args[0]
	balance := e.getBalance(username)
	
	return fmt.Sprintf("%s's balance: %s", username, e.formatMoney(balance))
}

func (e *EconomyPlugin) moneyCommand(args []string) string {
	if len(args) < 3 {
		return "Usage: /money <give|take|set> <player> <amount>"
	}
	
	action := args[0]
	username := args[1]
	amount, err := strconv.ParseFloat(args[2], 64)
	if err != nil {
		return "Invalid amount!"
	}
	
	switch strings.ToLower(action) {
	case "give":
		if e.addMoney(username, amount) {
			return fmt.Sprintf("Added %s to %s's account", e.formatMoney(amount), username)
		}
		return "Failed to add money!"
		
	case "take":
		if e.subtractMoney(username, amount) {
			return fmt.Sprintf("Removed %s from %s's account", e.formatMoney(amount), username)
		}
		return "Failed to remove money!"
		
	case "set":
		if e.setBalance(username, amount) {
			return fmt.Sprintf("Set %s's balance to %s", username, e.formatMoney(amount))
		}
		return "Failed to set balance!"
		
	default:
		return "Invalid action! Use: give, take, or set"
	}
}

func (e *EconomyPlugin) payCommand(args []string) string {
	if len(args) < 3 {
		return "Usage: /pay <player> <amount>"
	}
	
	sender := "CurrentPlayer"
	recipient := args[0]
	amount, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return "Invalid amount!"
	}
	
	if e.transferMoney(sender, recipient, amount) {
		return fmt.Sprintf("Paid %s to %s", e.formatMoney(amount), recipient)
	}
	
	return "Payment failed! Check your balance."
}

func (e *EconomyPlugin) economyCommand(args []string) string {
	if len(args) == 0 {
		return fmt.Sprintf("Economy Plugin v%s\nTotal players: %d\nCurrency: %s",
			e.version, len(e.playerData), e.config.CurrencyName)
	}
	
	switch strings.ToLower(args[0]) {
	case "reload":
		e.loadConfig()
		e.loadPlayerData()
		return "Economy configuration reloaded!"
		
	case "save":
		e.savePlayerData()
		return "Economy data saved!"
		
	case "stats":
		totalMoney := 0.0
		for _, account := range e.playerData {
			totalMoney += account.Balance
		}
		return fmt.Sprintf("Economy Statistics:\nTotal Players: %d\nTotal Money in Economy: %s\nAverage Balance: %s",
			len(e.playerData), e.formatMoney(totalMoney), e.formatMoney(totalMoney/float64(len(e.playerData))))
		
	default:
		return "Invalid economy command!"
	}
}

func (e *EconomyPlugin) topCommand(args []string) string {
	if len(e.topPlayers) == 0 {
		return "No players found!"
	}
	
	result := "Top Players by Balance:\n"
	for i, player := range e.topPlayers {
		result += fmt.Sprintf("%d. %s - %s\n", i+1, player.Username, e.formatMoney(player.Balance))
	}
	
	return result
}

func main() {
	plugin := NewEconomyPlugin()
	
	plugin.OnEnable()
	
	fmt.Println("\n=== Demo Commands ===")
	fmt.Println(plugin.balanceCommand([]string{"TestPlayer"}))
	fmt.Println(plugin.moneyCommand([]string{"give", "TestPlayer", "500"}))
	fmt.Println(plugin.balanceCommand([]string{"TestPlayer"}))
	fmt.Println(plugin.moneyCommand([]string{"give", "Player2", "2000"}))
	fmt.Println(plugin.topCommand([]string{}))
	fmt.Println(plugin.economyCommand([]string{"stats"}))
	
	plugin.OnDisable()
}
