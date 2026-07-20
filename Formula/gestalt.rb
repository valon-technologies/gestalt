# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.10"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.10/gestalt-macos-arm64.tar.gz"
      sha256 "549770da7162b29aa7f883a8989a65eab64f7f7cb552a6182cbf31f7787b481b"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.10/gestalt-macos-x86_64.tar.gz"
      sha256 "fd402dcb3d47d5b64477cf06c797b098cba11d5f488fe38c3fa76b55c64e94bd"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.10/gestalt-linux-arm64.tar.gz"
      sha256 "5fc6e9f65fd1313af5fcbe3a1f374ec6dcda68ff88be47998538933d5220717a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.10/gestalt-linux-x86_64.tar.gz"
      sha256 "d0aa34d9c55bcd637c599674119cc0c23b855aa4d88ecb3b8606d7ee3374fceb"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
