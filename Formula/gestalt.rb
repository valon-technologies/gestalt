# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.2"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.2/gestalt-macos-arm64.tar.gz"
      sha256 "5abf23c8466570f0a51d5e6e9067f92c14157025e09872e6c16994fe5af6d088"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.2/gestalt-macos-x86_64.tar.gz"
      sha256 "1e6b3f336aaa153f6773524243388126ba9362a768639aef5329ea049c0b9009"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.2/gestalt-linux-arm64.tar.gz"
      sha256 "89f10706395d88a1ff71db65790a05a25fe9553ccf7fd484acb89ad41bdd71e8"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.2/gestalt-linux-x86_64.tar.gz"
      sha256 "8b3ec2e9cf8c200a7b4a18d4b3bb675584883a3dc0aede5257a1006e6e2de87f"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
