# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.14"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.14/gestalt-macos-arm64.tar.gz"
      sha256 "9083c6b80c511c3fea64bd20992397051cd15d4087951419a582cb79424d3302"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.14/gestalt-macos-x86_64.tar.gz"
      sha256 "20a274ae57a136b189c10bb836e48049dec6439e7ddbd4fd1a6c0e4ff46566f9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.14/gestalt-linux-arm64.tar.gz"
      sha256 "d9007088fe58f118e5d3286a69ec2b9777e7afde1aeac093d44d4564eee52f11"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.14/gestalt-linux-x86_64.tar.gz"
      sha256 "ba9418b2ba54a78c2675110b54e8ddc623163e34aa3f714183b4777cccd62b0c"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
