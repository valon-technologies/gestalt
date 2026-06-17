# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.5"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.5/gestalt-macos-arm64.tar.gz"
      sha256 "686835f28c7053276f71a1fd6adbbdb2286fe47fe39e791de76d645b2aa3396c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.5/gestalt-macos-x86_64.tar.gz"
      sha256 "0041a06d87bd8f923be10c914d83e8750b34b97ee2d8162b7ffcecf46f359599"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.5/gestalt-linux-arm64.tar.gz"
      sha256 "23fa7a1d221e1dc7609c83b4a185ac813ac8f8678b7439ae2314f2d201f56237"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.5/gestalt-linux-x86_64.tar.gz"
      sha256 "ea9cd186515be270b44755b924e79e988607c22d92a45b88d9153f7c553dcf3d"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
