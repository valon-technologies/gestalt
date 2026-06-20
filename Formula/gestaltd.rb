# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.23"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.23/gestaltd-macos-arm64.tar.gz"
      sha256 "f91a34fe6d5d76d177654810b952b877c4c68454e85d194d66a92b79a30b861f"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.23/gestaltd-macos-x86_64.tar.gz"
      sha256 "9b3ace12f85acf87417648de1f9505da3c0d4e19789c983186f391dff7db06ae"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.23/gestaltd-linux-arm64.tar.gz"
      sha256 "984927af3e073b9babaa215912debbb456401a4d9a87a755f99f7405ebed3700"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.23/gestaltd-linux-x86_64.tar.gz"
      sha256 "5d36f64e865cb1901a6a113cc624eea55cba7bb1c5e49f8406de28c5a9074f97"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
