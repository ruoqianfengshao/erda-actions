package conf

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/gommon/random"
	"github.com/pkg/errors"

	"github.com/erda-project/erda-actions/actions/buildpack-aliyun/1.0/internal/run/langdetect/types"
	"github.com/erda-project/erda/pkg/envconf"
	"github.com/erda-project/erda/pkg/strutil"
)

// params 用户在 action.params 下声明的参数，获取 ACTION_ 前缀环境变量
type params struct {
	OrgID        int64  `env:"DICE_ORG_ID" required:"true"`
	OrgName      string `env:"DICE_ORG_NAME" required:"true"`
	ProjectID    int64  `env:"DICE_PROJECT_ID" required:"true"`
	ProjectName  string `env:"DICE_PROJECT_NAME" required:"true"`
	AppID        int64  `env:"DICE_APPLICATION_ID" required:"true"`
	AppName      string `env:"DICE_APPLICATION_NAME" required:"true"`
	GittarBranch string `env:"GITTAR_BRANCH"`

	// Language is your code language, like java, node, dockerfile...
	// Auto detect if empty.
	// +optional
	Language types.Language `env:"ACTION_LANGUAGE"`

	// BuildType means use what kind of build tool to build your code.
	// Often bundled with language. But you can declare this param only.
	// +optional
	BuildType types.BuildType `env:"ACTION_BUILD_TYPE"`

	// ContainerType means use what kind of pack tool to pack to image.
	// Often bundled with language/buildType. But you can declare this param only.
	// +optional
	ContainerType types.ContainerType `env:"ACTION_CONTAINER_TYPE"`

	// Context is the dir to execute build & pack commands, always same with your local build & pack.
	// +required
	Context string `env:"ACTION_CONTEXT" required:"true"`

	// Modules one-one mapping to services in dice.yml.
	// You can declare one or more modules, which depends on your lang(bp).
	// +required
	Modules    []*Module
	ModulesStr string `env:"ACTION_MODULES" required:"true"`

	HttpProxy  string `env:"ACTION_HTTP_PROXY" required:"false"`
	HttpsProxy string `env:"ACTION_HTTPS_PROXY" required:"false"`

	// OnlyBuild means no pack step.
	// +optional
	OnlyBuild bool `env:"ACTION_ONLY_BUILD"`

	// TODO
	// Deprecated
	BpRepo string `env:"ACTION_BP_REPO"`
	// Deprecated
	BpVer          string `env:"ACTION_BP_VER"`
	BpArgsStr      string `env:"ACTION_BP_ARGS"`
	BpArgs         map[string]string
	ForceBuildpack bool `env:"ACTION_FORCE_BUILDPACK"`

	JavaOpts string `env:"ACTION_JAVA_OPTS"`
	BuildkitEnable string `env:"BUILDKIT_ENABLE"`
}

type Module struct {
	// +required
	Name string `json:"name"`

	Path string `json:"path"`

	// Image and Images cannot both be null. If both have value, ignore Image.
	// TODO temporary only image
	Image  Image   `json:"image"`
	Images []Image `json:"images"`
}

type Image struct {
	// AutoGenerated represents if imageName is auto generated
	AutoGenerated bool `json:"autoGenerated"`

	// +required
	Name string `json:"name"`

	Username string `json:"username"`
	Password string `json:"password"`
}

func (p *params) FullLanguageInfo() bool {
	return p.Language != "" && p.BuildType != "" && p.ContainerType != ""
}

func initParams() (*params, error) {
	var p params
	if err := envconf.Load(&p); err != nil {
		return nil, errors.Errorf("failed to parse params: %v", err)
	}

	// bp config: bp_args/bp_repo/bp_ver
	if err := handleParamsBpConfig(&p); err != nil {
		return nil, err
	}

	// modules
	if err := handleParamsModules(&p); err != nil {
		return nil, err
	}

	// detect
	if err := detectLang(&p); err != nil {
		return nil, err
	}

	return &p, nil
}

func handleParamsBpConfig(p *params) error {
	// temp bp_args
	tempBpArgs := make(map[string]interface{})
	// bp_args
	if p.BpArgsStr != "" {
		if err := json.Unmarshal([]byte(p.BpArgsStr), &tempBpArgs); err != nil {
			return errors.Errorf("invalid bp_args, data: %s, err: %v", p.BpArgsStr, err)
		}
	}
	if p.BpArgs == nil {
		p.BpArgs = make(map[string]string)
	}
	// map[string]interface{} -> map[string]string
	for k, v := range tempBpArgs {
		switch v.(type) {
		case string:
			p.BpArgs[k] = v.(string)
		default:
			p.BpArgs[k] = fmt.Sprintf("%v", v)
		}
	}

	// 处理私有配置，全部转换为 bp_args
	handlePipelineSecretEnvsToBpArgs(p.BpArgs)
	// 设置默认 bp_args
	handleDefaultBpArgs(p.BpArgs)
	// 设置 http_proxy & https_proxy
	handleHttpProxyBpArgs(p.BpArgs, p)

	// bp_repo
	p.BpRepo = transferBpRepo(p.BpRepo)

	// compatible bp_repo/bp_ver to language/build_type/container_type
	compatibleExplicitBpRepoVer(p)

	return nil
}

// handlePipelineSecretEnvsToBpArgs 将私有配置生成的环境变量 转换为 bp_args
func handlePipelineSecretEnvsToBpArgs(bpArgs map[string]string) {
	for _, kv := range os.Environ() {
		s := strings.SplitN(kv, "=", 2)
		if len(kv) < 2 {
			continue
		}
		k := s[0]
		v := s[1]

		const pipelineSecretPrefix = "PIPELINE_SECRET_"

		if strings.HasPrefix(k, pipelineSecretPrefix) {
			bpArgs[strutil.TrimPrefixes(k, pipelineSecretPrefix)] = v
		}
	}
}

// 1. USE_AGENT
// 2. DICE_WORKSPACE
func handleDefaultBpArgs(bpArgs map[string]string) {
	const (
		USE_AGENT      = "USE_AGENT"
		DICE_WORKSPACE = "DICE_WORKSPACE"
	)

	// USE_AGENT
	if _, ok := bpArgs[USE_AGENT]; !ok {
		bpArgs[USE_AGENT] = "true"
	}

	// DICE_WORKSPACE
	bpArgs[DICE_WORKSPACE] = cfg.platformEnvs.DiceWorkspace
}

// handleHttpProxyBpArgs 处理 http proxy & https proxy
func handleHttpProxyBpArgs(bpArgs map[string]string, p *params) {
	if p.HttpProxy != "" {
		bpArgs["HTTP_PROXY"] = p.HttpProxy
	}
	if p.HttpsProxy != "" {
		bpArgs["HTTPS_PROXY"] = p.HttpsProxy
	}
}

func transferBpRepo(repo string) string {
	const buildpackDirInsideAction = "file:///opt/action/buildpacks/"

	// bp_repo 使用 Action 镜像内置的 bp
	gitlabHTTP := "http://git.terminus.io/buildpacks/"
	gitlabSSH := "git@git.terminus.io:buildpacks/"
	repo = strings.Replace(repo, gitlabHTTP, buildpackDirInsideAction, -1)
	repo = strings.Replace(repo, gitlabSSH, buildpackDirInsideAction, -1)
	return repo
}

func handleParamsModules(p *params) error {
	if p.ModulesStr != "" {
		if err := json.Unmarshal([]byte(p.ModulesStr), &p.Modules); err != nil {
			return errors.Errorf("invalid modules, data: %s, err: %v", p.ModulesStr, err)
		}
	}
	if len(p.Modules) == 0 {
		return errors.Errorf("missing modules")
	}
	for i, m := range p.Modules {
		if m.Name == "" {
			return errors.Errorf("missing modules[%d].name", i)
		}
		if m.Path == "" {
			m.Path = m.Name
		}
		if m.Image.Name == "" {
			repository := cfg.platformEnvs.ProjectAppAbbr
			if repository == "" {
				repository = fmt.Sprintf("%s/%s", "mock-user-id", random.String(8, random.Lowercase, random.Numeric))
			}
			tag := fmt.Sprintf("%s-%v", m.Name, time.Now().UnixNano())
			m.Image.Name = strings.ToLower(fmt.Sprintf("%s/%s:%s", filepath.Clean(cfg.platformEnvs.BpDockerArtifactRegistry), repository, tag))
			m.Image.AutoGenerated = true
		}
		if m.Image.Username != "" && m.Image.Password == "" {
			return errors.Errorf("source.modules[%d].image.password cannot be empty when username exist", i)
		}
		if m.Image.Username == "" && m.Image.Password != "" {
			return errors.Errorf("source.modules[%d].image.username cannot be empty when password exist", i)
		}
		// check docker login
		if m.Image.Username != "" {
			// login
			getRegistry := func(image string) string { return strings.Split(image, "/")[0] }
			login := exec.Command("docker", "login", "-u", m.Image.Username, "-p", m.Image.Password, getRegistry(m.Image.Name))
			login.Stdout = os.Stdout
			login.Stderr = os.Stderr
			if err := login.Run(); err != nil {
				return errors.Wrapf(err, "docker login failed, source.modules[%d].image, username: %s, password: %s", i, m.Image.Username, m.Image.Password)
			}
		}
		m.Path = filepath.Clean(m.Path)
	}

	return nil
}

// compatibleExplicitBpRepoVer 兼容用户显式通过 bp_repo/bp_ver 指定的情况，转换为 language/build_type/build_container 配置
func compatibleExplicitBpRepoVer(p *params) {
	if p.BpRepo == "" {
		return
	}

	if strutil.Contains(p.BpRepo, "java") {
		p.Language = types.LanguageJava
		p.BuildType = types.BuildTypeMaven
		p.ContainerType = types.ContainerTypeSpringBoot

		// edas
		if strutil.Contains(p.BpVer, "edas") {
			if strutil.Contains(p.BpVer, "dubbo") {
				p.BuildType = types.BuildTypeMavenEdasDubbo
				p.ContainerType = types.ContainerTypeEdasDubbo
				return
			}
			p.BuildType = types.BuildTypeMavenEdas
			p.ContainerType = types.ContainerTypeEdas
		}
	}

	if strutil.Contains(p.BpRepo, "nodejs") {
		p.Language = types.LanguageNode
		p.BuildType = types.BuildTypeNpm
		p.ContainerType = types.ContainerTypeHerd
	}

	if strutil.Contains(p.BpRepo, "spa") {
		p.Language = types.LanguageNode
		p.BuildType = types.BuildTypeNpm
		p.ContainerType = types.ContainerTypeSpa
	}

	if strutil.Contains(p.BpRepo, "dockerimage") {
		p.Language = types.LanguageDockerfile
		p.BuildType = types.BuildTypeDockerfile
		p.ContainerType = types.ContainerTypeDockerfile
	}
}
