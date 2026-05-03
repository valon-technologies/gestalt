# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.12"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.12/gestalt-macos-arm64.tar.gz"
      sha256 "55223889a1034afd2e2e5207c0e3356470867c559830169f08ee86e98114790c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.12/gestalt-macos-x86_64.tar.gz"
      sha256 "f36bc36531d20e11373d07f66df735e532f963228a11b12ca1c5b90cc93cc445"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.12/gestalt-linux-arm64.tar.gz"
      sha256 "5df2520c8fb2ff293efb3310e673100444636476d12769f7b7259844fdeb3254"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.1-alpha.12/gestalt-linux-x86_64.tar.gz"
      sha256 "30fbce6d2410f8f2a1dd7845b49c1df7015cdcb26dbcf5758aa95c1f4cd9d7c2"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
