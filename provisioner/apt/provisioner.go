package apt

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

type Provisioner struct {
	config Config
	comm   packer.Communicator
}

func (p *Provisioner) ConfigSpec() hcldec.ObjectSpec { return p.config.FlatMapstructure().HCL2Spec() }

func (p *Provisioner) Prepare(raws ...interface{}) error {
	return p.config.Prepare(raws...)
}

func (p *Provisioner) Provision(ctx context.Context, ui packer.Ui, comm packer.Communicator, _ map[string]interface{}) error {
	ui.Say("Provisioning with APT...")

	if err := p.uploadHostPackageCache(ui, comm); err != nil {
		ui.Error(fmt.Sprintf("Failed to upload APT cache from %s", p.config.CacheDir))
		return err
	}

	if err := p.uploadHostPackageTrust(ui, comm); err != nil {
		return err
	}

	if err := p.testRemoteDNS(ctx, ui, comm); err != nil {
		ui.Error("Failed waiting for domain name resolution")
		return err
	}

	if len(p.config.Sources) != 0 {
		if err := p.uploadPackageList(ui, comm); err != nil {
			ui.Error("Failed to upload APT package list")
			return err
		}
		if err := p.updateRemotePackageIndex(ctx, ui, comm); err != nil {
			ui.Error("apt-get update failed")
			return err
		}
	}

	if err := p.installRemotePackages(ctx, ui, comm); err != nil {
		ui.Error("apt-get install failed.")
		return err
	}

	if err := p.updateCache(ui, comm); err != nil {
		return err
	}

	if err := p.cleanRemotePackages(ctx, ui, comm); err != nil {
		ui.Error("apt-get clean failed, ignoring")
		return err
	}

	return nil
}

func (p *Provisioner) updateCache(ui packer.Ui, comm packer.Communicator) error {
	_, err := os.Stat(p.config.CacheDir)
	if os.IsNotExist(err) {
		ui.Say("Skipping updating package cache, likely not running on a debian based host.")
		return nil
	} else if err != nil {
		return err
	}

	dir, err := ioutil.TempDir(os.TempDir(), "archives-")
	if err != nil {
		ui.Error("APT cache update: failed to create tempdir")
		return err
	}
	defer os.RemoveAll(dir)

	if err := comm.DownloadDir("/var/cache/apt/archives", dir, []string{}); err != nil {
		ui.Error(fmt.Sprintf("APT cache update: failed to download archives to %s", dir))
		return err
	}

	cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf("mv -n %s/*.deb %s", dir, p.config.CacheDir))
	if err := cmd.Run(); err != nil {
		ui.Error(fmt.Sprintf("APT cache update: mv: %v", err))
		return err
	}

	return nil
}

func (p *Provisioner) uploadHostPackageCache(ui packer.Ui, comm packer.Communicator) error {
	cache, err := os.Stat(p.config.CacheDir)
	if os.IsNotExist(err) {
		ui.Say("Host APT package cache not found, likely not running on a debian based host. Proceeding regardless")
		return nil
	} else if err != nil {
		return err
	}

	if err == nil && cache.IsDir() {
		err := comm.UploadDir("/var/cache/apt/archives", p.config.CacheDir, []string{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Provisioner) uploadHostPackageTrust(ui packer.Ui, comm packer.Communicator) error {
	for _, key := range p.config.Keys {
		f, err := os.Open(key)
		if os.IsNotExist(err) {
			ui.Say(fmt.Sprintf("Package trust key '%s' doesn't exist, likely not running on a debian based host. Skipping transfer.", key))
			continue
		} else if err != nil {
			return err
		}
		defer f.Close()

		fi, err := f.Stat()

		if os.IsNotExist(err) {
			ui.Say(fmt.Sprintf("Package trust key '%s' doesn't exist, likely not running on a debian based host. Skipping transfer.", key))
			continue
		} else if err != nil {
			return err
		}

		err = comm.Upload("/etc/apt/trusted.gpg.d/"+filepath.Base(key), f, &fi)
		if err != nil {
			ui.Error(fmt.Sprintf("Failed to upload APT key %s", key))
			return err
		}
	}
	return nil
}

func (p *Provisioner) uploadPackageList(ui packer.Ui, comm packer.Communicator) error {
	r := strings.NewReader(strings.Join(p.config.Sources, "\n") + "\n")
	err := comm.Upload("/etc/apt/sources.list.d/packer.list", r, nil)
	if err != nil {
		return err
	}
	return nil
}

func (p *Provisioner) updateRemotePackageIndex(ctx context.Context, ui packer.Ui, comm packer.Communicator) error {
	cmd := &packer.RemoteCmd{Command: "/usr/bin/apt-get update"}
	err := cmd.RunWithUi(ctx, comm, ui)
	if err != nil {
		return err
	}
	return nil
}

func (p *Provisioner) installRemotePackages(ctx context.Context, ui packer.Ui, comm packer.Communicator) error {
	cmd := &packer.RemoteCmd{
		Command: fmt.Sprintf(
			"DEBIAN_FRONTEND=noninteractive /usr/bin/apt-get install -y --no-install-recommends %s",
			strings.Join(p.config.Packages, " "),
		),
	}
	if err := cmd.RunWithUi(ctx, comm, ui); err != nil {
		return err
	}
	return nil
}

func (p *Provisioner) testRemoteDNS(ctx context.Context, ui packer.Ui, comm packer.Communicator) error {
	cmd := &packer.RemoteCmd{
		Command: "/bin/sh -c 'for i in $(seq 100); do " +
			"resolvectl query deb.debian.org >/dev/null && break; sleep 0.1; done; " +
			"resolvectl query deb.debian.org'",
	}
	if err := cmd.RunWithUi(ctx, comm, ui); err != nil {
		return err
	}
	return nil
}

func (p *Provisioner) cleanRemotePackages(ctx context.Context, ui packer.Ui, comm packer.Communicator) error {
	cmd := &packer.RemoteCmd{Command: "/usr/bin/apt-get clean"}
	if err := cmd.RunWithUi(ctx, comm, ui); err != nil {
		return err
	}
	return nil
}
