# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-macos-arm64.tar.gz"
      sha256 "3ac7c6ad8d4e36eb8d7fad56468d9e49b131c5d44190bcacf8e2a17325bdb43c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-macos-x86_64.tar.gz"
      sha256 "bedccf006aa3ddafeb0309e333c4540dcc12036c4883a07496bc914902796725"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-linux-arm64.tar.gz"
      sha256 "1a8063b7d70e12fa78390139eb9283baee250dacc451a3bb9280af8c6b7fbcbf"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1/gestaltd-linux-x86_64.tar.gz"
      sha256 "9e55ded757429ad2a8264e0b0e7a379976fcb1783a150da3e35de3180177140e"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
