package main

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	vlcctrl "github.com/CedArctic/go-vlc-ctrl"
)

func main() {
	// load config
	config, err := ReadConfig()
	if err != nil {
		log.Fatalf("read config error: %v", err)
	}
	log.Printf("config: %v", config)

	// start vlc
	err = StartVLC(config.VLC)
	if err != nil {
		log.Fatalf("start vlc error: %v", err)
	}
	log.Printf("start vlc success")

	// new vlc control instance
	instance, _ := vlcctrl.NewVLC("127.0.0.1", 8080, "password")

	// restore playing last item
	if config.Playing != "" {
		// 因为vlc可能还没启动成功，因此重试3秒
		start := time.Now()
		for time.Now().Sub(start) <= 3*time.Second {
			err = instance.AddStart(config.Playing)
			if err == nil {
				break
			}
			log.Printf("add error: %v", err)
			time.Sleep(500 * time.Millisecond)
		}

		err = instance.Seek(strconv.Itoa(config.Position))
		if err != nil {
			log.Printf("seek error: %v", err)
		}

		time.Sleep(1 * time.Second)
		err = instance.ToggleFullscreen()
		if err != nil {
			log.Printf("seek error: %v", err)
		}

		AddRelatedItems(instance, config.Playing)
		Skip(instance, config.Position, config.BeginSkip, config.EndSkip)
	}

	for {
		time.Sleep(4 * time.Second)

		// 获取正在播放的项目
		file, pos, _, err := GetPlayingItem(instance)
		if err != nil {
			log.Printf("GetPlayingItem error: %v", err)
			continue
		}

		// 把相关项目添加到播放列表
		AddRelatedItems(instance, file)

		Skip(instance, pos, config.BeginSkip, config.EndSkip)

		// 更新config
		if file != config.Playing || config.Position != pos {
			config.Playing = file
			config.Position = pos
			config.Save()
		}
	}
}

type Config struct {
	VLC       string // vlc可执行程序路径
	Playing   string // 最后播放的文件URI
	Position  int    //  播放的位置，单位是秒
	BeginSkip int
	EndSkip   int
}

const defaultConfig = "./config.json"

func ReadConfig() (*Config, error) {
	config := &Config{}

	b, err := os.ReadFile(defaultConfig)
	if err == os.ErrNotExist {
		config.VLC = "D:/scoop/apps/vlc/3.0.20/vlc.exe"
		config.Save()
		return config, nil
	} else if err != nil {
		return nil, err
	}

	err = json.Unmarshal(b, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (c *Config) Save() error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(defaultConfig, b, 0644)
}

// StartVLC 启动vlc进程
func StartVLC(vlc string) error {
	return exec.Command(vlc, "--intf", "http", "--extraintf", "qt", "--http-password", "password").Start()
}

// GetPlayingItem 获取正在播放的项目及其进度
func GetPlayingItem(instance vlcctrl.VLC) (file string, pos int, list []string, err error) {
	// 获取正在播放的项目
	playlist, err := instance.Playlist()
	if err != nil {
		log.Printf("playlist error: %v", err)
		return
	}
	// log.Printf("playlist: %#v", playlist)
	// vlcctrl.Node{Ro:"rw", Type:"node", Name:"", ID:"0", Duration:0, URI:"", Current:"", Children:[]vlcctrl.Node{vlcctrl.Node{Ro:"ro", Type:"node", Name:"Playlist", ID:"1", Duration:0, URI:"", Current:"", Children:[]vlcctrl.Node{vlcctrl.Node{Ro:"rw", Type:"leaf", Name:"1.mp4", ID:"15", Duration:5, URI:"file:///D:/desktop/1.mp4", Current:"current", Children:[]vlcctrl.Node(nil)}}}, vlcctrl.Node{Ro:"ro", Type:"node", Name:"Media Library", ID:"2", Duration:0, URI:"", Current:"", Children:[]vlcctrl.Node{}}}}

	// 获取当前播放的项目
	// TODO 要支持递归遍历？
	for _, item := range playlist.Children[0].Children {
		if item.Current == "current" {
			file = item.URI
			list = append(list, file)
			// pos = item.Duration
			// break
		}
	}

	// 获取正在播放项目的进度
	resp, err := instance.RequestMaker("/requests/status.json")
	if err != nil {
		log.Printf("GetStatus error: %v", err)
		return
	}
	// log.Printf("GetStatus: %v", resp)

	var status Status
	err = json.Unmarshal([]byte(resp), &status)
	if err != nil {
		log.Printf("status json.unmarshal error: %v", err)
		return
	}

	// 获取当前播放的时间，单位是秒
	pos = status.Time
	return
}

type Status struct {
	// FullScreen bool `json:"fullscreen"`
	Time   int `json:"time"`
	Length int `json:"length"`
}

// 缓存上次添加相关项目的路径
var dirAlreadyAdded string

// 把当前正在播放项目的相关项目添加到播放列表
func AddRelatedItems(instance vlcctrl.VLC, current string) error {
	current = filepath.FromSlash(strings.TrimPrefix(current, "file:///"))
	dir := filepath.Dir(current)
	// 如果目录已添加，则无需再次添加
	if dir == dirAlreadyAdded {
		return nil
	}
	ext := filepath.Ext(current)
	log.Printf("AddRelatedItems: current: %v, dir: %v, ext: %v", current, dir, ext)

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("readdir error: %v", err)
		return err
	}

	// instance.EmptyPlaylist()
	start := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ext {
			continue
		}
		add := filepath.Join(dir, entry.Name())
		if add == current {
			start = true
			continue
		}
		if !start {
			continue
		}
		err = instance.Add(add)
		if err != nil {
			return err
		} else {
			log.Printf("add item: %v", add)
		}
	}

	dirAlreadyAdded = dir
	log.Printf("dirAlreadyAdded: %v", dirAlreadyAdded)
	return nil
}

// Skip 调过片头和片尾
func Skip(instance vlcctrl.VLC, pos, beginSkip, endSkip int) {
	if beginSkip != 0 && pos < beginSkip {
		instance.Seek(strconv.Itoa(beginSkip))
	} else if endSkip != 0 && pos > endSkip {
		instance.Next()
		instance.Seek(strconv.Itoa(beginSkip))
	}
}
