package main

import (
  "os"
  "fmt"
  "bufio"
  "strings"
  "runtime"
  "os/exec"
  "path/filepath"
  "time"
  "bytes"
  "regexp"
  "github.com/fatih/color"
)

type arguments struct {
  is_git bool
  main_path string
  config_path string
  log_path string
  keys []string
  values []string
}

type optional_config struct {
  keys []string
  values []string
}

type config struct {
  name string
  password string
  version string
  distribution string
  release string
  architecture string
  mountpoint string
  keys []string
  values []string
}

type toml struct {
  keys []string
  values []string
}

func main() {
  if runtime.GOOS != "linux" {
    die("this program only works on linux")
  }

  arguments := get_args(os.Args[1:])

  log("arguments parsed")

  var optional_config optional_config
  if arguments.config_path != "" {
    optional_config = get_optional_config(arguments.config_path)
    log("optional config read")
  }

  config := get_config(arguments.main_path + "/config.toml")

  log("config read")

  config = overwrite_config(config, optional_config.keys, optional_config.values)
  config = overwrite_config(config, arguments.keys, arguments.values)

  log("final config is set")

  var err error
  err = os.MkdirAll(config.mountpoint, os.ModePerm)
  check(err, "could not create + '" + config.mountpoint + "'")

  log("mountpoint created at " + config.mountpoint)

  switch config.distribution {
  case "alpine":
  case "debian":
  case "ubuntu":
  default:
    die("'" + config.distribution + "' distribution not supported")
  }

  exec_command("lxc-create", "--name", config.name, "--template", "download", "--", "-d", config.distribution, "-r", config.release, "-a", config.architecture)

  log(config.name + " linux container has been created")

  err = os.MkdirAll("/var/log/lxc", os.ModePerm)
  check(err, "could not create '/var/log/lxc'")

  lxc_config := 
  "lxc.mount.entry = " + config.mountpoint + " mnt none bind 0 0\n" +
  "lxc.start.auto = 1\n" +
  "lxc.console.logfile = /var/log/lxc/" + config.name + ".conf\n" +
  "lxc.console.buffer.size = 128kB"

  write_to_file("/var/lib/lxc/" + config.name + "/config", lxc_config)

  log("lxc config created")

  exec_command("lxc-start", "--name", config.name)
  exec_command("lxc-wait", "--name", config.name, "--state", "RUNNING")

  log("container started")

  var ip string
  for ok := true; ok; ok = ip == "" {
    ip = exec_command("lxc-info", "--name", config.name, "-iH")
    time.Sleep(1)
  }

  ip = strings.TrimSuffix(ip, "\n")

  log("network connected")

  switch config.distribution {
  case "alpine":
    lxc_exec_command(config.name, "apk update && apk upgrade")
    lxc_exec_command(config.name, "apk add --no-cache python3 openssh-server")
  case "debian":
    lxc_exec_command(config.name, "apt update && apt upgrade -y")
    lxc_exec_command(config.name, "apt install -y python3 openssh-server")
  case "ubuntu":
    lxc_exec_command(config.name, "apt update && apt upgrade -y")
    lxc_exec_command(config.name, "apt install -y python3 openssh-server")
  }

  log("container fully upgraded")

  lxc_exec_command(config.name, "yes " + config.password + " | passwd")
  lxc_exec_command(config.name, "sed -i 's/^#\\?\\s*PermitRootLogin .*$/PermitRootLogin yes/' /etc/ssh/sshd_config")
  lxc_exec_command(config.name, "rc-update add sshd")
  lxc_exec_command(config.name, "/etc/init.d/sshd start")

  log("container sshd configured")

  exec_command("ansible-galaxy", "collection", "install", "community.general")

  ansible_inventory :=
  "[all:vars]\n" +
  "ansible_connection=ssh\n" +
  "ansible_user=root\n" +
  "ansible_ssh_pass=" + config.password + "\n" +
  "ansible_ssh_common_args=\"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null\"\n" +
  "ansible_python_interpreter=/usr/bin/python3\n"

  for i := 0; i < len(config.keys); i++ {
    ansible_inventory += config.keys[i] + "=" + config.values[i] + "\n"
  }

  ansible_inventory += 
  "[all]\n" +
  ip + "\n"

  write_to_file(arguments.main_path + "/inventory.ini", ansible_inventory)

  log("ansible inventory created")

  cmd := exec.Command("ansible-playbook", "-i", arguments.main_path + "/inventory.ini", arguments.main_path + "/playbook.yaml")
  var o bytes.Buffer
  cmd.Stdout = &o
  // cmd.Env = append(cmd.Env, "ANSIBLE_HOST_KEY_CHECKING=false")
  err = cmd.Run()
  if err != nil {
    os.Remove(arguments.main_path + "/inventory.ini")
    if arguments.is_git {
      os.Remove(arguments.main_path)
    }
    die("'" + arguments.main_path + "/playbook.yaml' failed: " + err.Error() + "\n" + string(o.Bytes()))
  }
  output := string(o.Bytes())

  if arguments.log_path != "" {
    write_to_file(arguments.log_path, string(output))
  }

  log("ansible finished")

  os.Remove(arguments.main_path + "/inventory.ini")
  if arguments.is_git {
    os.Remove(arguments.main_path)
  }

  log(config.name + " linux container is installed")
}

func lxc_exec_command(name string, command string) string {
  cmd := exec.Command("lxc-attach", "--name", name)

  var i bytes.Buffer
  i.WriteString(command)
  cmd.Stdin = &i
  var o bytes.Buffer
  cmd.Stdout = &o

  err := cmd.Run()
  if err != nil {
    die("'" + command + "' failed: " + err.Error())
  }

  output := string(o.Bytes())
  return output
}

func write_to_file(path string, content string) {
  var err error
  // get parent directory
  parent := filepath.Dir(path)
  err = os.MkdirAll(parent, os.ModePerm)
  check(err, "could not create + '" + parent + "'")

  // writing to file
  var file *os.File
  file, err = os.OpenFile(path, os.O_APPEND | os.O_WRONLY | os.O_CREATE, 0600)
  check(err, "could not create + '" + path + "'")

  _, err = file.WriteString(content)
  check(err, "could not write to '" + path + "'")

  file.Close()
}

func exec_command(args ...string) string {
  cmd := exec.Command(args[0], args[1:]...)
  out, err := cmd.CombinedOutput()
  if err != nil {
    command := strings.Join(args, " ")
    die("'" + command + "' failed: " + err.Error() + "\n" + string(out))
  }
  output := string(out)
  return output
}

func exec_command_env(env string, args ...string) string {
  cmd := exec.Command(args[0], args[1:]...)
  var o bytes.Buffer
  cmd.Stdout = &o
  cmd.Env = append(cmd.Env, env)
  err := cmd.Run()
  if err != nil {
    command := strings.Join(args, " ")
    die("'" + command + "' failed: " + err.Error() + "\n" + string(o.Bytes()))
  }
  output := string(o.Bytes())
  return output
}

func overwrite_config(config config, keys []string, values []string) config {
  for i := 0; i < len(keys); i++ {
    if keys[i] == "name" {
      config.name = values[i]
    }
    if keys[i] == "password" {
      config.password = values[i]
    }
    if keys[i] == "version" {
      config.version = values[i]
    }
    if keys[i] == "distribution" {
      config.distribution = values[i]
    }
    if keys[i] == "release" {
      config.release = values[i]
    }
    if keys[i] == "architecture" {
      config.architecture = values[i]
    }
    if keys[i] == "mountpoint" {
      config.mountpoint = values[i]
    }
    for j := 0; j < len(config.keys); j++ {
      if keys[i] == config.keys[j] {
        config.values[j] = values[i]
      }
    }
  }
  return config
}

func get_config(path string) config {
  var config config

  config_data, err := os.ReadFile(path)
  check(err, "'" + path + "' no such file or directory")

  toml := toml_parse(string(config_data))

  for i := 0; i < len(toml.keys); i++ {
    if toml.keys[i] == "name" {
      config.name = toml.values[i]
      continue
    }
    if toml.keys[i] == "password" {
      config.password = toml.values[i]
      continue
    }
    if toml.keys[i] == "version" {
      config.version = toml.values[i]
      continue
    }
    if toml.keys[i] == "distribution" {
      config.distribution = toml.values[i]
      continue
    }
    if toml.keys[i] == "release" {
      config.release = toml.values[i]
      continue
    }
    if toml.keys[i] == "architecture" {
      config.architecture = toml.values[i]
      continue
    }
    if toml.keys[i] == "mountpoint" {
      config.mountpoint = toml.values[i]
      continue
    }
    config.keys = append(config.keys, toml.keys[i])
    config.values = append(config.values, toml.values[i])
  }

  if config.name == "" {
    die("'" + path + "' missing name")
  }
  if config.password == "" {
    die("'" + path + "' missing password")
  }
  if config.version == "" {
    die("'" + path + "' missing version")
  }
  if config.distribution == "" {
    die("'" + path + "' missing distribution")
  }
  if config.release == "" {
    die("'" + path + "' missing release")
  }
  if config.architecture == "" {
    die("'" + path + "' missing architecture")
  }
  if config.mountpoint == "" {
    die("'" + path + "' missing mountpoint")
  }

  return config
}

func get_optional_config(path string) optional_config {
  var optional_config optional_config

  config_data, err := os.ReadFile(path)
  check(err, "'" + path + "' no such file or directory")

  toml := toml_parse(string(config_data))

  for i := 0; i < len(toml.keys); i++ {
    optional_config.keys = append(optional_config.keys, toml.keys[i])
    optional_config.values = append(optional_config.values, toml.values[i])
  }

  return optional_config
}

func toml_parse(str string) toml {
  var toml toml
  scanner := bufio.NewScanner(strings.NewReader(str))
  for scanner.Scan() {
    line := scanner.Text()
    key := toml_get_key(line)
    if key == "" {
      continue
    }
    value := toml_get_value(line)
    if value == "" {
      continue
    }
    toml.keys = append(toml.keys, key)
    toml.values = append(toml.values, value)
  }
  return toml
}

func toml_get_key(str string) string {
  trimed := trim(str, ' ')

  if trimed == "" {
    return ""
  }

  if trimed[0] == '#' {
    return ""
  }

  if trimed[0] == '[' || trimed[len(trimed) - 1] == ']' {
    return ""
  }

  end := -1
  for i := 1; i < len(trimed); i++ {
    if trimed[i] == '=' {
      end = i
    }
  }

  if end == -1 {
    die("invalid toml '" + str + "'")
  }

  key := trim(trimed[0:end], ' ')

  return key
}

func toml_get_value(str string) string {
  trimed := trim(str, ' ')

  if trimed == "" {
    return ""
  }

  if trimed[0] == '#' {
    return ""
  }

  if trimed[0] == '[' || trimed[len(trimed) - 1] == ']' {
    return ""
  }

  start := -1
  for i := 0; i < len(trimed) - 3; i++ {
    if trimed[i] == '=' {
      start = i
    }
  }

  if start == -1 {
    die("invalid toml '" + str + "'")
  }

  value := trim(trimed[start + 1:len(trimed)], ' ')
  value = trim(value[1:len(value) - 1], ' ')

  return value
}

func trim(str string, del byte) string {
  if str == "" {
    return ""
  }
  start := -1 
  for i := 0; i < len(str); i++ {
    if str[i] != del {
      start = i
      break
    }
  }
  if start == -1 {
    return ""
  }
  end := -1
  for i := len(str) - 1; i >= 0; i-- {
    if str[i] != del {
      end = i
      break
    }
  }
  return str[start:end + 1]
}

func inv_trim(str string, del byte) string {
  if str == "" {
    return ""
  }
  start := -1 
  for i := 0; i < len(str); i++ {
    if str[i] == del {
      start = i
      break
    }
  }
  if start == -1 {
    return ""
  }
  end := -1
  for i := len(str) - 1; i >= 0; i-- {
    if str[i] == del {
      end = i
      break
    }
  }
  return str[start + 1:end]
}

func get_args(args []string) arguments {
  var arguments arguments
  var err error

  var arg_done []bool
  for i := 0; i < len(args); i++ {
    arg_done = append(arg_done, false)
  }

  if len(args) < 1 {
    die("enter a package name for the linux container")
  }

  // get path of linux container template
  path := args[len(args) - 1]
  arg_done[len(arg_done) - 1] = true

  match_http, _ := regexp.MatchString("^https?://.+\\..+/.+", path)
  match_git, _ := regexp.MatchString("^git@.+\\..+:.+", path)

  if match_http || match_git {
    err = os.RemoveAll("/tmp/lxc-install")
    check(err, "could not remote '/tmp/lxc-install'")
    err = os.MkdirAll("/tmp/lxc-install", os.ModePerm)
    check(err, "could not create '/tmp/lxc-install'")
    exec_command("git", "clone", path, "/tmp/lxc-install")
    arguments.main_path = "/tmp/lxc-install"
    arguments.is_git = true
  } else {
    arguments.main_path = path
    arguments.is_git = false
  }

  _, err = os.Stat(arguments.main_path)
  check(err, "'" + arguments.main_path + "' no such file or directory")

  _, err = os.Stat(arguments.main_path + "/config.toml")
  check(err, "'" + arguments.main_path + "/config.toml' no such file or directory")

  _, err = os.Stat(arguments.main_path + "/playbook.yaml")
  check(err, "'" + arguments.main_path + "/playbook.yaml' no such file or directory")

  // get custom config
  for i := 0; i < len(args) - 2; i++ {
    if args[i] == "-c" {
      arguments.config_path = args[i + 1]
      arg_done[i] = true
      arg_done[i + 1] = true
      break
    }
  }

  if arguments.config_path != "" {
    _, err = os.Stat(arguments.config_path)
    check(err, "'" + arguments.config_path + "' no such file or directory")
  }

  for i := 0; i < len(args) - 2; i++ {
    if args[i] == "-l" {
      arguments.log_path = args[i + 1]
      arg_done[i] = true
      arg_done[i + 1] = true
      break
    }
  }

  // get custom variables
  for i := 0; i < len(args) - 2; i++ {
    if args[i][0:2] == "--" {
      arguments.keys = append(arguments.keys, args[i][2:])
      arguments.values = append(arguments.values, args[i + 1])
      arg_done[i] = true
      arg_done[i + 1] = true
      i++
    }
  }

  for i := 0; i < len(arg_done); i++ {
    if !arg_done[i] {
      die("'" + args[i] + "' invalid argument")
    }
  }
  return arguments
}

func check(err error, message string) {
  if err != nil {
    die(message)
  }
}

func warn(message string) {
  c := color.New(color.FgRed)
  fmt.Fprintf(os.Stderr, "[")
  c.Fprintf(os.Stderr, " FAIL ")
  fmt.Fprintf(os.Stderr, "] - %s\n", message)
}

func log(message string) {
  c := color.New(color.FgGreen)
  fmt.Printf("[")
  c.Printf("  OK  ")
  fmt.Printf("] - %s\n", message)
}

func die(message string) {
  warn(message)
  os.Exit(1)
}
