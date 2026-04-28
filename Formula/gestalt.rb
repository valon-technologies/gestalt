# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.8"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.8/gestalt-macos-arm64.tar.gz"
      sha256 "a088d53b25ce8db4bdd91d0c83b31a22d4f3cb069d92054cfb0dc7c155bf0bc8"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.8/gestalt-macos-x86_64.tar.gz"
      sha256 "2d1124e3903b04a21aa18e901fe7207a0664da37c567626a22acc48e49daa72c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.8/gestalt-linux-arm64.tar.gz"
      sha256 "895b36a34b4df552fedc6ffdc1228d08a5f762dffca9bb332817b0ef324bca39"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.8/gestalt-linux-x86_64.tar.gz"
      sha256 "c88d0770b6a3301b20f3f82fa54af0c08bd1f5e271c96c95f26bc7c40614f68c"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
