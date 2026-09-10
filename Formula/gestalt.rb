# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.19"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.19/gestalt-macos-arm64.tar.gz"
      sha256 "ce0eaba09a6d88d1c2b8d31478d5ccf9e882c451cf63d03f2e88736715354c14"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.19/gestalt-macos-x86_64.tar.gz"
      sha256 "6128c7b999ed264b57329b221980463589d323ed4cf9a9da19276e6ceee43053"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.19/gestalt-linux-arm64.tar.gz"
      sha256 "40e07d33c58b7343ad8438524df481f17e5a38753a44c63b4f70c54ab2be49b3"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.19/gestalt-linux-x86_64.tar.gz"
      sha256 "3529be59370e26ee412fb62b5ce7eeac2235df344d56ee8823e4f8532a7e217d"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
