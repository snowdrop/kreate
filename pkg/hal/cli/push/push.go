package push

import (
	"bufio"
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	component "halkyon.io/api/component/v1beta1"
	"halkyon.io/hal/pkg/cmdutil"
	"halkyon.io/hal/pkg/k8s"
	"halkyon.io/hal/pkg/log"
	"hash/crc64"
	"io"
	"io/ioutil"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record/util"
	"os"
	"path/filepath"
	"strings"
)

const commandName = "push"

type options struct {
	*cmdutil.ComponentTargetingOptions
}

func (o *options) Complete(name string, cmd *cobra.Command, args []string) error {
	return nil
}

func (o *options) Validate() error {
	return nil
}

func (o *options) Run() error {
	c := k8s.GetClient()
	component, err := c.HalkyonComponentClient.Components(c.Namespace).Get(o.ComponentName, v1.GetOptions{})
	if err != nil {
		// check error to see if it means that the component doesn't exist yet
		if util.IsKeyNotFoundError(errors.Cause(err)) {
			// the component was not found so we need to create it first and wait for it to be ready
			log.Infof("Component %s was not found, initializing it", o.ComponentName)
			err = k8s.Apply(o.DescriptorPath, c.Namespace)
			if err != nil {
				return fmt.Errorf("error applying component CR: %v", err)
			}

			component, err = o.waitUntilReady(component)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// check if the component revision is different
	binaryPath, err := o.getComponentBinaryPath()
	if err != nil {
		return fmt.Errorf("couldn't find binary to push: %s", binaryPath)
	}
	file, err := os.Open(binaryPath)
	if err != nil {
		return err
	}
	input := bufio.NewReader(file)
	hash := crc64.New(crc64.MakeTable(crc64.ECMA))
	if _, err := io.Copy(hash, input); err != nil {
		return err
	}
	revision := fmt.Sprintf("%x", hash.Sum(nil))
	if revision == component.Spec.Revision {
		log.Info("No local changes detected: nothing to push!")
		return nil
	}

	// we got the component, we still need to check it's ready
	component, err = o.waitUntilReady(component)
	if err != nil {
		return err
	}
	err = o.push(component)
	if err != nil {
		return err
	}

	// update the component revision
	patch := fmt.Sprintf(`{"spec":{"revision":"%s"}}`, revision)
	_, err = c.HalkyonComponentClient.Components(c.Namespace).
		Patch(o.ComponentName, types.MergePatchType, []byte(patch))
	if err != nil {
		return err
	}
	return nil

}

func (o *options) waitUntilReady(c *component.Component) (*component.Component, error) {
	if component.ComponentReady == c.Status.Phase || component.ComponentRunning == c.Status.Phase {
		return c, nil
	}

	client := k8s.GetClient()
	cp, err := client.WaitForComponent(o.ComponentName, component.ComponentReady, "Waiting for component "+o.ComponentName+" to be ready…")
	if err != nil {
		return nil, fmt.Errorf("error waiting for component: %v", err)
	}
	err = errorIfFailedOrUnknown(c)
	if err != nil {
		return nil, err
	}
	return cp, nil
}

func errorIfFailedOrUnknown(c *component.Component) error {
	switch c.Status.Phase {
	case component.ComponentFailed, component.ComponentUnknown:
		return errors.Errorf("status of component %s is %s: %s", c.Name, c.Status.Phase, c.Status.Message)
	default:
		return nil
	}
}

func (o *options) push(component *component.Component) error {
	c := k8s.GetClient()
	podName := component.Status.PodName
	/*// todo: fix copy function
	err = c.CopyFile(".", podName, "/deployments", []string{"target/" + app + "-0.0.1-SNAPSHOT.jar"}, nil)
	if err != nil {
		return err
	}*/
	jar, _ := o.getComponentBinaryPath()
	s := log.Spinner("Uploading " + jar)
	defer s.End(false)
	err := k8s.Copy(jar, c.Namespace, podName)
	if err != nil {
		return fmt.Errorf("error uploading jar: %v", err)
	}
	s.End(true)
	// use pipes to write output from ExecCMDInContainer in yellow  to 'out' io.Writer
	pipeReader, pipeWriter := io.Pipe()
	var cmdOutput string
	// This Go routine will automatically pipe the output from ExecCMDInContainer to
	// our logger.
	go func() {
		scanner := bufio.NewScanner(pipeReader)
		for scanner.Scan() {
			line := scanner.Text()
			cmdOutput += fmt.Sprintln(line)
		}
	}()
	err = c.ExecCMDInContainer(podName, []string{"/var/lib/supervisord/bin/supervisord", "ctl", "stop", "run-java"}, pipeWriter, pipeWriter, nil, false)
	if err != nil {
		return err
	}
	err = c.ExecCMDInContainer(podName, []string{"/var/lib/supervisord/bin/supervisord", "ctl", "start", "run-java"}, pipeWriter, pipeWriter, nil, false)
	if err != nil {
		return err
	}
	return nil
}

func (o *options) getComponentBinaryPath() (string, error) {
	target := filepath.Join(o.ComponentPath, "target")
	files, err := ioutil.ReadDir(target)
	if err != nil {
		return target + " directory not found or unreadable", err
	}

	for _, file := range files {
		name := file.Name()
		if strings.HasSuffix(name, ".jar") {
			return filepath.Join(target, name), nil
		}
	}
	return "no jar file found in " + target, nil
}

func (o *options) SetTargetingOptions(options *cmdutil.ComponentTargetingOptions) {
	o.ComponentTargetingOptions = options
}

func NewCmdPush(parent string) *cobra.Command {
	push := &cobra.Command{
		Use:   fmt.Sprintf("%s [flags]", commandName),
		Short: "Push a local project to the remote cluster you're connected to",
		Long:  `Push a local project to the remote cluster you're connected to.`,
		Args:  cobra.NoArgs,
	}
	cmdutil.ConfigureRunnableAndCommandWithTargeting(&options{}, push)
	return push
}