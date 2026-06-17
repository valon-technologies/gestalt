# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.20"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.20/gestaltd-macos-arm64.tar.gz"
      sha256 "a8c62f34687f1813ae33049509f5b434bc54fbfde5bac8306f0ecfde4e9306b9"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.20/gestaltd-macos-x86_64.tar.gz"
      sha256 "bb4fe431273ebbd029539f07e28de0951b88cf5f038aa9c2f01deaead1278534"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.20/gestaltd-linux-arm64.tar.gz"
      sha256 "f5cae9c2871eaebb5cdc9030a95456f0a95cc3830ee4df53977841e37db6e969"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.20/gestaltd-linux-x86_64.tar.gz"
      sha256 "d3b5985338e864621c004a18c67375bba74575e8e2a940a5642679c2b5bbaef5"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
